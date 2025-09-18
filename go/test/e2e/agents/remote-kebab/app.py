import logging
import click
import uvicorn

from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import (
    AgentCapabilities,
    AgentCard,
    AgentSkill,
    UnsupportedOperationError,
)
from a2a.types import Message, TextPart, Task, TaskState, TaskStatus, TaskStatusUpdateEvent
import uuid
from datetime import datetime, timezone
from starlette.applications import Starlette
from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.utils.errors import ServerError

logging.basicConfig(level=logging.DEBUG)

class KebabAgentExecutor(AgentExecutor):
    """A simple AgentExecutor that responds with kebab message."""

    def __init__(self, card: AgentCard):
        self._card = card

    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
        """Execute the kebab agent with proper Task history for RecordingManager."""
        # Get the user id if passed through headers
        cc = getattr(context, 'call_context', None)
        state = getattr(cc, 'state', None) if cc is not None else None
        headers = state.get('headers', {}) if isinstance(state, dict) else {}
        user_id = headers.get('x-user-id') or 'unknown'

        # Create kebab response with user info
        response_text = f"kebab for {user_id} in session {context.context_id}"
        
        # Create agent response message
        agent_message = Message(
            context_id=context.context_id,
            message_id=str(uuid.uuid4()),
            task_id=context.task_id,
            kind="message",
            role="agent",
            parts=[TextPart(text=response_text)]
        )

        # Build task history [user, agent] (for test)
        history = []
        if context.message:
            history.append(context.message)
        history.append(agent_message)

        task = Task(
            id=context.task_id,
            context_id=context.context_id,
            status=TaskStatus(
                state=TaskState.working,
                message=agent_message,
                timestamp=datetime.now(timezone.utc).isoformat()
            ),
            history=history
        )
        await event_queue.enqueue_event(task)
        
        status_update = TaskStatusUpdateEvent(
            task_id=context.task_id,
            context_id=context.context_id,
            status=TaskStatus(
                state=TaskState.completed,
                message=agent_message,
                timestamp=datetime.now(timezone.utc).isoformat()
            ),
            final=False 
        )
        await event_queue.enqueue_event(status_update)
        
        logging.debug('[Kebab Agent] execute completed with task history')

    async def cancel(self, context: RequestContext, event_queue: EventQueue):
        raise ServerError(error=UnsupportedOperationError())

@click.command()
@click.option('--host', 'host', default='0.0.0.0')
@click.option('--port', 'port', default=8080)
def main(host: str, port: int):
    skill = AgentSkill(
        id='kebab_response',
        name='Kebab Response',
        description='Responds with kebab for any input',
        tags=['kebab', 'response'],
        examples=[
            'Say kebab',
        ],
    )

    # AgentCard for kebab agent
    agent_card = AgentCard(
        name='remote_kebab_agent',
        description='Kebab agent that responds with kebab when invoked',
        url=f'http://remote-kebab-agent.remote.svc.cluster.local:8080/',
        version='0.1.0',
        defaultInputModes=['text'],
        defaultOutputModes=['text'],
        capabilities=AgentCapabilities(streaming=True),
        skills=[skill],
    )

    # Create kebab agent executor
    agent_executor = KebabAgentExecutor(card=agent_card)

    request_handler = DefaultRequestHandler(
        agent_executor=agent_executor, task_store=InMemoryTaskStore()
    )

    a2a_app = A2AStarletteApplication(
        agent_card=agent_card, http_handler=request_handler
    )
    routes = a2a_app.routes()

    app = Starlette(routes=routes)

    uvicorn.run(app, host=host, port=port, log_level="debug")

if __name__ == '__main__':
    main()
