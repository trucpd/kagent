import logging
import os
import click
import uvicorn

from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import (
    AgentCapabilities,
    AgentCard,
    AgentSkill,
)
from starlette.applications import Starlette
from a2a.server.agent_execution import AgentExecutor
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.server.tasks import TaskUpdater
from a2a.types import (
    TaskState,
    TextPart,
    UnsupportedOperationError,
)
from a2a.utils.errors import ServerError

logging.basicConfig(level=logging.DEBUG)

class KebabAgentExecutor(AgentExecutor):
    """An AgentExecutor that responds with kebab."""

    def __init__(self, card: AgentCard):
        self._card = card

    async def _process_request(
        self,
        message_text: str,
        context: RequestContext,
        task_updater: TaskUpdater,
    ) -> None:
        # get the user id if passed through headers
        cc = getattr(context, 'call_context', None)
        state = getattr(cc, 'state', None) if cc is not None else None
        headers = state.get('headers', {}) if isinstance(state, dict) else {}
        user_id = headers.get('x-user-id') or 'unknown'

        response_text = f"kebab for {user_id} in session {context.context_id}"

        # First add as artifact so it is captured for sync Task history
        parts = [TextPart(text=response_text)]
        await task_updater.add_artifact(parts)

        # Then emit a final status with the agent message (for streaming and completion)
        await task_updater.update_status(
            TaskState.completed,
            message=task_updater.new_agent_message(parts),
            final=True,
        )

    async def execute(
        self,
        context: RequestContext,
        event_queue: EventQueue,
    ):
        # Run the agent until complete
        updater = TaskUpdater(event_queue, context.task_id, context.context_id)
        # Immediately notify that the task is submitted.
        if not context.current_task:
            await updater.submit()
        await updater.start_work()

        # Extract text from message parts
        message_text = ''
        for part in context.message.parts:
            if isinstance(part.root, TextPart):
                message_text += part.root.text

        await self._process_request(message_text, context, updater)
        logging.debug('[Kebab Agent] execute exiting')

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
        url=f'http://{host}:{port}/',
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
