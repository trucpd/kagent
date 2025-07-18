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
import uvicorn

from pydantic import ValidationError
import pydantic

from fastapi.websockets import WebSocketDisconnect
from fastapi.websockets import WebSocket
from fastapi import FastAPI
from fastapi.responses import HTMLResponse
from typing import Optional
from typing_extensions import override
from typing import Any
import asyncio, traceback
from google.genai import types
import httpx

logger = logging.getLogger("kagent." + __name__)

class AgentRunRequest(pydantic.BaseModel):
    app_name: str
    user_id: str
    session_id: str
    new_message: types.Content

class KagentSessionService(BaseSessionService):
    """A session service implementation that uses the Kagent API.

    This service integrates with the Kagent server to manage session state
    and persistence through HTTP API calls.
    """

    def __init__(self, kagent_url: str):
        super().__init__()
        self.kagent_url = kagent_url.rstrip("/")
        self.client = httpx.AsyncClient()

    async def _get_user_id(self) -> str:
        """Get the default user ID. Override this method to implement custom user ID logic."""
        return "default-user"

    @override
    async def create_session(
        self,
        *,
        app_name: str,
        user_id: str,
        state: Optional[dict[str, Any]] = None,
        session_id: Optional[str] = None,
    ) -> Session:
        # Prepare request data
        request_data = {
            "user_id": user_id,
            "agent_ref": app_name,  # Use app_name as agent reference
        }
        if session_id:
            request_data["name"] = session_id

        # Make API call to create session
        response = await self.client.post(
            f"{self.kagent_url}/api/sessions",
            json=request_data,
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

        data = response.json()
        if not data.get("data"):
            raise RuntimeError(
                f"Failed to create session: {data.get('message', 'Unknown error')}"
            )

        session_data = data["data"]

        # Convert to ADK Session format
        return Session(
            id=session_data["id"], user_id=session_data["user_id"], state=state or {}
        )

    @override
    async def get_session(
        self,
        *,
        app_name: str,
        user_id: str,
        session_id: str,
        config: Optional[GetSessionConfig] = None,
    ) -> Optional[Session]:
        try:
            # Make API call to get session
            response = await self.client.get(
                f"{self.kagent_url}/api/sessions/{session_id}?user_id={user_id}",
                headers={"X-User-ID": user_id},
            )
            response.raise_for_status()

            data = response.json()
            if not data.get("data"):
                return None

            session_data = data["data"]

            # Convert to ADK Session format
            return Session(
                id=session_data["id"],
                user_id=session_data["user_id"],
                app_name="todo",
                state={},  # TODO: restore State
            )
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return None
            raise

    @override
    async def list_sessions(
        self, *, app_name: str, user_id: str
    ) -> ListSessionsResponse:
        # Make API call to list sessions
        response = await self.client.get(
            f"{self.kagent_url}/api/sessions", headers={"X-User-ID": user_id}
        )
        response.raise_for_status()

        data = response.json()
        sessions_data = data.get("data", [])

        # Convert to ADK Session format
        sessions = []
        for session_data in sessions_data:
            session = Session(
                id=session_data["id"], user_id=session_data["user_id"], state={}
            )
            sessions.append(session)

        return ListSessionsResponse(sessions=sessions)

    def list_sessions_sync(
        self, *, app_name: str, user_id: str
    ) -> ListSessionsResponse:
        raise NotImplementedError("not supported. use async")

    @override
    async def delete_session(
        self, *, app_name: str, user_id: str, session_id: str
    ) -> None:
        # Make API call to delete session
        response = await self.client.delete(
            f"{self.kagent_url}/api/sessions/{session_id}",
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

    @override
    async def append_event(self, session: Session, event: Event) -> Event:
        # Convert ADK Event to JSON format
        event_data = {
            "event": {
                "type": event.__class__.__name__,
                "data": (
                    event.model_dump()
                    if hasattr(event, "model_dump")
                    else event.__dict__
                ),
            }
        }

        # Make API call to append event to session
        response = await self.client.post(
            f"{self.kagent_url}/api/sessions/{session.id}/events?user_id={session.user_id}",
            json=event_data,
            headers={"X-User-ID": session.user_id},
        )
        response.raise_for_status()

        return event


class KagentAdkAgent:

    def __init__(self, agent: BaseAgent):
        self.agent = agent
        self.session_service = KagentSessionService(kagent_url="http://localhost:8083")
        self.runner = self._create_runner_async(self.agent.name)

    async def session_stream(self, session: Session) -> None:
        pass

    def _create_runner_async(self, app_name: str) -> Runner:
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
        self.runner: Runner = (
            runner  # Use the passed runner instead of creating a new one
        )
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
        async for event in self.runner.run_live(user_id = self.session.user_id,
            session_id=self.session.id, live_request_queue=self.live_request_queue
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

    async def run(self, req: AgentRunRequest) -> list[Event]:
        events = [
            event
            async for event in self.runner.run_async(
                user_id=req.user_id,
                session_id=req.session_id,
                new_message=req.new_message,
            )
        ]
        logger.info("Generated %s events in agent run: %s", len(events), events)
        return events

async def run_server(agent: BaseAgent):
    # Run the FastAPI server.
    kagent_agent = KagentAdkAgent(agent)
    app = FastAPI()

    @app.get("/")
    async def get():
        return HTMLResponse(
            "<html><body><h1>Welcome to Kagent Agent Server</h1></body></html>"
        )

    @app.post("/run", response_model_exclude_none=True)
    async def agent_run(req: AgentRunRequest) -> list[Event]:
        session = await kagent_agent.session_service.get_session(
            session_id=req.session_id, app_name=req.app_name, user_id=req.user_id
        )
        h = SessionHandler(None, kagent_agent.agent, kagent_agent.runner, session)
        return await h.run(req)

    @app.websocket("/run_live")
    async def agent_live_run(
        websocket: WebSocket,
        app_name: str,
        user_id: str,
        session_id: str,
        # modalities: List[Literal["TEXT", "AUDIO"]] = Query(
        #    default=["TEXT", "AUDIO"]
        # ),  # Only allows "TEXT" or "AUDIO"
    ) -> None:
        await websocket.accept()
        session = await kagent_agent.session_service.get_session(
            session_id=session_id, app_name=app_name, user_id=user_id
        )
        if session is None:
            await websocket.close(code=1008, reason="Session not found")
            return

        h = SessionHandler(websocket, kagent_agent.agent, kagent_agent.runner, session)
        await h.run_live()

    config = uvicorn.Config(
        app,
        host="0.0.0.0",
        port=8000,
        #    reload=reload,
    )

    server = uvicorn.Server(config)
    await server.serve()


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
async def main():
    print("Hello from agent-py!")
    await run_server(root_agent)
    # Example usage of KagentSessionService:
    #
    # from google.adk.sessions import Session
    # from google.adk.events.event import Event
    #
    # async def demo_session_service():
    #     service = KagentSessionService("http://localhost:8000")
    #
    #     # Create a session
    #     session = await service.create_session(
    #         app_name="my-agent",
    #         user_id="user123",
    #         session_id="demo-session"
    #     )
    #
    #     # Get session
    #     retrieved_session = await service.get_session(
    #         app_name="my-agent",
    #         user_id="user123",
    #         session_id="demo-session"
    #     )
    #
    #     # List sessions
    #     sessions_response = await service.list_sessions(
    #         app_name="my-agent",
    #         user_id="user123"
    #     )
    #
    #     # Append an event to the session
    #     from google.adk.events.event import Event
    #     event = Event()  # Create your event
    #     await service.append_event(session, event)
    #
    #     # Delete session
    #     await service.delete_session(
    #         app_name="my-agent",
    #         user_id="user123",
    #         session_id="demo-session"
    #     )


if __name__ == "__main__":
    asyncio.run(main())


test_with_curl = """

curl -i --header "Upgrade: websocket" \
--header "Connection: Upgrade" \
--header "Sec-WebSocket-Key: YSBzYW1wbGUgMTYgYnl0ZQ==" \
--header "Sec-Websocket-Version: 13" \
localhost:8000/run_live

"""