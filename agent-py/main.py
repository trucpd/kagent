import json
import logging
from agent import root_agent
from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import AgentCard

from google.adk.runners import Runner
from google.adk.sessions import Session
from google.adk.sessions.base_session_service import (
    BaseSessionService,
    GetSessionConfig,
    ListSessionsResponse,
)
from google.adk.events.event import Event
from google.adk.agents.live_request_queue import LiveRequestQueue, LiveRequest
from google.adk.agents import BaseAgent

from pydantic import ValidationError

from fastapi.websockets import WebSocketDisconnect
from fastapi.websockets import WebSocket
from fastapi import FastAPI
from typing import Optional
from typing_extensions import override
from typing import Any

import asyncio,traceback

logger = logging.getLogger("kagent." + __name__)


class KagentSessionService(BaseSessionService):
    """An in-memory implementation of the session service.

    It is not suitable for multi-threaded production environments. Use it for
    testing and development only.
    """

    def __init__(self, kagent_url: str):
        super().__init__()
        self.kagent_url = kagent_url
        pass

    @override
    async def create_session(
        self,
        *,
        app_name: str,
        user_id: str,
        state: Optional[dict[str, Any]] = None,
        session_id: Optional[str] = None,
    ) -> Session:
        raise NotImplementedError("TODO")

    @override
    async def get_session(
        self,
        *,
        app_name: str,
        user_id: str,
        session_id: str,
        config: Optional[GetSessionConfig] = None,
    ) -> Optional[Session]:
        raise NotImplementedError("TODO")

    @override
    async def list_sessions(
        self, *, app_name: str, user_id: str
    ) -> ListSessionsResponse:
        raise NotImplementedError("TODO")

    def list_sessions_sync(
        self, *, app_name: str, user_id: str
    ) -> ListSessionsResponse:
        raise NotImplementedError("not supported. use async")

    @override
    async def delete_session(
        self, *, app_name: str, user_id: str, session_id: str
    ) -> None:
        raise NotImplementedError("TODO")

    @override
    async def append_event(self, session: Session, event: Event) -> Event:
        raise NotImplementedError("TODO")


class KagentAdkAgent:

    def __init__(self, agent: BaseAgent):
        self.agent = agent
        self.session_service = KagentSessionService(kagent_url="http://localhost:8000")
        self.runner = self._create_runner_async(self.agent.name)

    async def session_stream(self, session: Session) -> None:
        pass

    async def _create_runner_async(self, app_name: str) -> Runner:
        # envs.load_dotenv_for_agent(os.path.basename(app_name), agents_dir)
        runner = Runner(
            app_name=app_name,
            agent=self.agent,
            artifact_service=None,
            session_service=self.session_service,
            memory_service=None,
            credential_service=None,
        )
        return runner

class SessionHandler:

    def __init__(
        self, websocket: WebSocket, agent: BaseAgent, runner: Runner, session: Session
    ):
        self.websocket: WebSocket = websocket
        self.agent: BaseAgent = agent
        self.session: Session = session
        self.runner: Runner = self._get_runner_async(self.agent.name)
        self.live_request_queue: LiveRequestQueue = LiveRequestQueue()

    async def run_live(self) -> None:
        # Run both tasks concurrently and cancel all if one fails.
        tasks = [
            asyncio.create_task(self.forward_events()),
            asyncio.create_task(self.process_messages()),
        ]
        done, pending = await asyncio.wait(tasks, return_when=asyncio.FIRST_EXCEPTION)
        try:
            # This will re-raise any exception from the completed tasks.
            for task in done:
                task.result()
        except WebSocketDisconnect:
            logger.info("Client disconnected during process_messages.")
        except Exception as e:
            logger.exception("Error during live websocket communication: %s", e)
            traceback.print_exc()
            WEBSOCKET_INTERNAL_ERROR_CODE = 1011
            WEBSOCKET_MAX_BYTES_FOR_REASON = 123
            await self.websocket.close(
                code=WEBSOCKET_INTERNAL_ERROR_CODE,
                reason=str(e)[:WEBSOCKET_MAX_BYTES_FOR_REASON],
            )
        finally:
            for task in pending:
                task.cancel()

    async def forward_events(self):
        async for event in self.runner.run_live(
            session=self.session, live_request_queue=self.live_request_queue
        ):
            await self.websocket.send_text(
                event.model_dump_json(exclude_none=True, by_alias=True)
            )

    async def process_messages(self):
        try:
            while True:
                data = await self.websocket.receive_text()
                # Validate and send the received message to the live queue.
                self.live_request_queue.send(LiveRequest.model_validate_json(data))
        except ValidationError as ve:
            logger.error("Validation error in process_messages: %s", ve)

def run_server(agent: BaseAgent):
    # Run the FastAPI server.
    kagent_agent = KagentAdkAgent(agent)
    app = FastAPI(lifespan=internal_lifespan)

    @app.websocket("/run_live")
    async def agent_live_run(
        websocket: WebSocket,
        app_name: str,
        user_id: str,
        session_id: str,
        #modalities: List[Literal["TEXT", "AUDIO"]] = Query(
        #    default=["TEXT", "AUDIO"]
        #),  # Only allows "TEXT" or "AUDIO"
    ) -> None:
        await websocket.accept()
        session = await kagent_agent.session_service.get_session(session_id=session_id, app_name=app_name, user_id=user_id)
        h = SessionHandler(websocket, kagent_agent.agent, kagent_agent.runner, session)
        await kagent_agent.session_stream()

# async def run_agent():
#     host: str = "0.0.0.0"
#     port: int = 8000
#     app = FastAPI(lifespan=internal_lifespan)
#
#     def create_a2a_runner_loader(captured_app_name: str):
#         async def _get_a2a_runner_async() -> Runner:
#             return await _get_runner_async(captured_app_name)
#
#         return _get_a2a_runner_async
#
#     app_name = root_agent.name
#     logger.info("Setting up A2A agent: %s", app_name)
#
#     try:
#         a2a_rpc_path = f"http://{host}:{port}/a2a/{app_name}"
#
#         agent_executor = A2aAgentExecutor(
#             runner=create_a2a_runner_loader(app_name),
#         )
#
#         request_handler = DefaultRequestHandler(
#             agent_executor=agent_executor, task_store=a2a_task_store
#         )
#
#         with open("agent.json", "r", encoding="utf-8") as f:
#             data = json.load(f)
#             agent_card = AgentCard(**data)
#             agent_card.url = a2a_rpc_path
#
#         a2a_app = A2AStarletteApplication(
#             agent_card=agent_card,
#             http_handler=request_handler,
#         )
#
#         routes = a2a_app.routes(
#             rpc_url=f"/a2a/{app_name}",
#             agent_card_url=f"/a2a/{app_name}/.well-known/agent.json",
#         )
#
#         for new_route in routes:
#             app.router.routes.append(new_route)
#
#         logger.info("Successfully configured A2A agent: %s", app_name)
#
#     except Exception as e:
#         logger.error("Failed to setup A2A agent %s: %s", app_name, e)
def main():
    print("Hello from agent-py!")


if __name__ == "__main__":
    main()
