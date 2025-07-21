import logging
import os

import httpx
import uvicorn
from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.types import AgentCard, AgentCapabilities, AgentSkill
from a2a.utils.constants import AGENT_CARD_WELL_KNOWN_PATH
from fastapi import FastAPI
from fastapi.responses import HTMLResponse, PlainTextResponse
from google.adk.a2a.executor.a2a_agent_executor import A2aAgentExecutor
from google.adk.agents import BaseAgent
from google.adk.runners import Runner

from contextlib import asynccontextmanager

from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider

from .kagent_session_service import KagentSessionService
from .kagent_task_store import KAgentTaskStore

logger = logging.getLogger("kagent." + __name__)

class KagentServices:
    def __init__(self, kagent_url: str):
        client = httpx.AsyncClient(base_url=kagent_url)
        self.artifact_service = None  # Placeholder for artifact service
        self.session_service = KagentSessionService(client)
        self.memory_service = None  # Placeholder for memory service
        self.credential_service = None  # Placeholder for credential service
        self.task_store = KAgentTaskStore(client)

async def run_a2a_server(agent: BaseAgent):

    # this is the host it can be accessed from outside the container (for the agent card.)
    port = 8080
    host = os.environ.get("KAGENT_SERVICE_HOST")

    kagent_api_url = os.environ.get("KAGENT_API_URL")

    provider = TracerProvider()
    #    provider.add_span_processor(
    #        export.SimpleSpanProcessor(ApiServerSpanExporter(trace_dict))
    #    )
    #    memory_exporter = InMemoryExporter(session_trace_dict)
    #    provider.add_span_processor(export.SimpleSpanProcessor(memory_exporter))

    trace.set_tracer_provider(provider)
    app_name = agent.name

    # Run the FastAPI server.

    # httpx.AsyncClient(base_url=base_url.rstrip("/"))
    kagent_services = KagentServices(kagent_url=kagent_api_url)

    runner = Runner(
            app_name=app_name,
            agent=agent,
            artifact_service=kagent_services.artifact_service,
            session_service=kagent_services.session_service,
            memory_service=kagent_services.memory_service,
            credential_service=kagent_services.credential_service,
        )

    @asynccontextmanager
    async def agent_lifespan(app: FastAPI):
        yield
        await runner.close()

    app = FastAPI(lifespan=agent_lifespan)

    logger.info("Setting up A2A agent: %s", app_name)

    a2a_rpc_path = f"http://{host}:{port}/a2a/{app_name}"

    agent_executor = A2aAgentExecutor(runner=runner)

    request_handler = DefaultRequestHandler(
        agent_executor=agent_executor, task_store=kagent_services.task_store
    )

    agent_card = AgentCard(
        name=app_name,
        description=agent.description,
        version="1.0.0",
        url=a2a_rpc_path,
        capabilities= AgentCapabilities(
            streaming=True,
        ),
        defaultInputModes=["text"],
        defaultOutputModes=["text"],
        skills=[AgentSkill(description="TODO", id="TODO", name="TODO", tags=["TODO"])],
    )

    a2a_app = A2AStarletteApplication(
        agent_card=agent_card,
        http_handler=request_handler,
    )

    routes = a2a_app.routes(
        rpc_url=f"/a2a/{app_name}",
        agent_card_url=f"/a2a/{app_name}{AGENT_CARD_WELL_KNOWN_PATH}",
    )

    for new_route in routes:
        app.router.routes.append(new_route)

    logger.info("Successfully configured A2A agent: %s", app_name)

    @app.get("/")
    async def get():
        return HTMLResponse(
            f"<html><body><h1>Welcome to Kagent Agent Server</h1> Use A2A with this url: {a2a_rpc_path}</body></html>"
        )

    @app.get("/healthz")
    async def get():
        return PlainTextResponse(
            f"ok"
        )

    config = uvicorn.Config(
        app,
        host="0.0.0.0",
        port=8080,
        #    reload=reload,
    )

    server = uvicorn.Server(config)
    await server.serve()
