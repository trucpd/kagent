from fastapi import FastAPI

from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import AgentCard
from google.adk.a2a.executor.a2a_agent_executor import A2aAgentExecutor

app = FastAPI()


class A2aServer:
    def __init__(self):
        self.app = A2AStarletteApplication(
            request_handler=DefaultRequestHandler(),
            task_store=InMemoryTaskStore(),
        )
