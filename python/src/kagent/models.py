#! /usr/bin/env python3

import asyncio
import json
import logging
from typing import Self

from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import AgentCard
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from google.adk.a2a.executor.a2a_agent_executor import A2aAgentExecutor
from google.adk.agents import Agent
from google.adk.agents.llm_agent import ToolUnion
from google.adk.auth.auth_credential import AuthCredential
from google.adk.auth.auth_schemes import AuthScheme
from google.adk.models.lite_llm import LiteLlm
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.adk.tools.agent_tool import AgentTool
from google.adk.tools.mcp_tool import MCPToolset, SseConnectionParams, StreamableHTTPConnectionParams
from google.genai import types
from pydantic import BaseModel, Field

from kagent import KAgentSessionService, KAgentTaskStore


class HttpMcpServerConfig(BaseModel):
    params: StreamableHTTPConnectionParams
    tools: list[str] = Field(default_factory=list)


class SseMcpServerConfig(BaseModel):
    params: SseConnectionParams
    tools: list[str] = Field(default_factory=list)


class AgentConfig(BaseModel):
    kagent_url: str  # The URL of the KAgent server
    agent_card: AgentCard
    name: str
    model: str
    description: str
    instruction: str
    http_tools: list[HttpMcpServerConfig] | None = None  # tools, always MCP
    sse_tools: list[SseMcpServerConfig] | None = None  # tools, always MCP
    agents: list[Self] | None = None  # agent names

    def to_agent(self) -> Agent:
        mcp_toolsets: list[ToolUnion] = []
        if self.http_tools:
            for http_tool in self.http_tools:  # add http tools
                mcp_toolsets.append(MCPToolset(connection_params=http_tool.params, tool_filter=http_tool.tools))
        if self.sse_tools:
            for sse_tool in self.sse_tools:  # add stdio tools
                mcp_toolsets.append(MCPToolset(connection_params=sse_tool.params, tool_filter=sse_tool.tools))
        if self.agents:
            for agent in self.agents:  # Add sub agents as tools
                mcp_toolsets.append(AgentTool(agent.to_agent()))
        return Agent(
            name=self.name,
            model=self.model,
            description=self.description,
            instruction=self.instruction,
            tools=mcp_toolsets,
        )


class TaskConfig(BaseModel):
    root_agent: str
    agents: dict[str, AgentConfig]

    def to_agent(self) -> Agent:
        return self.agents[self.root_agent].to_agent()


# --- Constants ---
APP_NAME = "kagent"
USER_ID = "admin@kagent.dev"
SESSION_ID = "123344"

# --- Configure Logging ---
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


async def setup_session_and_runner(session_id: str, root_agent: Agent):
    session_service = InMemorySessionService()
    session = await session_service.create_session(
        app_name=APP_NAME,
        user_id=USER_ID,
        session_id=session_id,
    )
    logger.info(f"Initial session state: {session.state}")
    runner = Runner(
        agent=root_agent,
        app_name=APP_NAME,
        session_service=session_service,
    )
    return session_service, runner


def health_check(request: Request) -> PlainTextResponse:
    return PlainTextResponse("OK")


def build_app(filepath: str = "/config/config.json") -> FastAPI:
    with open(filepath, "r") as f:
        config = json.load(f)
    agent_config = AgentConfig.model_validate(config)
    root_agent = agent_config.to_agent()
    session_service = KAgentSessionService("http://kagent.kagent-dev.svc.cluster.local:8083")
    runner = Runner(
        agent=root_agent,
        app_name=APP_NAME,
        session_service=session_service,
    )

    agent_executor = A2aAgentExecutor(
        runner=runner,
    )

    kagent_task_store = KAgentTaskStore("http://kagent.kagent-dev.svc.cluster.local:8083")

    request_handler = DefaultRequestHandler(
        agent_executor=agent_executor,
        task_store=kagent_task_store,
    )

    a2a_app = A2AStarletteApplication(
        agent_card=agent_config.agent_card,
        http_handler=request_handler,
    )

    app = FastAPI()

    # Health check/readiness probe
    app.add_route("/", methods=["GET"], route=health_check)

    a2a_app.add_routes_to_app(app)

    return app
