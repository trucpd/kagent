#! /usr/bin/env python3

import asyncio
import json
import logging
from typing import Self

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


class HttpMcpServerConfig(BaseModel):
    params: StreamableHTTPConnectionParams
    tools: list[str] = Field(default_factory=list)


class SseMcpServerConfig(BaseModel):
    params: SseConnectionParams
    tools: list[str] = Field(default_factory=list)


class AgentConfig(BaseModel):
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


json_config = """
{
  "agents": null,
  "description": "A toolserver for math problems",
  "http_tools": [
    {
      "params": {
        "headers": {},
        "read_timeout": 300,
        "terminate_on_close": false,
        "timeout": 30,
        "url": "http://localhost:8084/mcp"
      },
      "tools": [
        "k8s_get_resources"
      ]
    }
  ],
  "instruction": "You are a kubernetes agent. You can use the k8s_get_resources tool to get resources from the kubernetes cluster.",
  "model": "gemini-2.0-flash",
  "name": "test__NS__agent",
  "sse_tools": null
}
"""


# --- Function to Interact with the Agent ---
async def call_agent_async(session_id: str, user_input_topic: str):
    """
    Sends a new topic to the agent (overwriting the initial one if needed)
    and runs the workflow.
    """

    agent_config = json.loads(json_config)
    agent_config = AgentConfig.model_validate(agent_config)

    session_service, runner = await setup_session_and_runner(session_id, agent_config.to_agent())

    current_session = await session_service.get_session(app_name=APP_NAME, user_id=USER_ID, session_id=session_id)

    if not current_session:
        logger.error("Session not found!")
        return

    current_session.state["topic"] = user_input_topic
    logger.info(f"Updated session state topic to: {user_input_topic}")

    content = types.Content(role="user", parts=[types.Part(text=user_input_topic)])
    events = runner.run_async(user_id=USER_ID, session_id=session_id, new_message=content)

    final_response = "No final response captured."
    async for event in events:
        logger.info(f"Event: {event}")
        if event.is_final_response() and event.content and event.content.parts:
            logger.info(f"Potential final response from [{event.author}]: {event.content.parts[0].text}")
            final_response = event.content.parts[0].text

    print("\n--- Agent Interaction Result ---")
    print("Agent Final Response: ", final_response)

    final_session = await session_service.get_session(app_name=APP_NAME, user_id=USER_ID, session_id=SESSION_ID)
    print("Final Session State:")

    print(json.dumps(final_session.state, indent=2))
    print("-------------------------------\n")


# --- Run the Agent ---
# Note: In Colab, you can directly use 'await' at the top level.
# If running this code as a standalone Python script, you'll need to use asyncio.run() or manage the event loop.


async def main():
    await call_agent_async(SESSION_ID, "list all pods in the default namespace")


asyncio.run(main())
