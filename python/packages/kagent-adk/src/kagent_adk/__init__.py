import importlib.metadata

from .a2a import KAgentApp
from .kagent_session_service import KAgentSessionService
from .kagent_task_store import KAgentTaskStore
from .models import AgentConfig

__version__ = importlib.metadata.version("kagent_adk")

__all__ = ["KAgentSessionService", "KAgentTaskStore", "KAgentApp", "AgentConfig"]
