import logging
import os
from authlib.jose import jwt
from authlib.jwk import jwk
import httpx

# --- Configure Logging ---
logger = logging.getLogger(__name__)


class JWTValidator:
    def __init__(self, jwks_url, issuer):
        self.jwks_url = jwks_url
        self.issuer = issuer
        self.keys = None

    async def fetch_keys(self):
        async with httpx.AsyncClient() as client:
            response = await client.get(self.jwks_url)
            self.keys = response.json()["keys"]

    async def validate(self, token):
        if not self.keys:
            await self.fetch_keys()

        claims = jwt.decode(token, jwk.loads(self.keys))
        claims.validate()

        if claims["iss"] != self.issuer:
            raise Exception("Invalid issuer")

        return claims


validator = JWTValidator(os.getenv("JWKS_URL"), os.getenv("ISSUER"))


class KAgentRequestContextBuilder(SimpleRequestContextBuilder):
    """
    A request context builder that will be used to hack in the user_id for now.
    """

    def __init__(self, task_store: TaskStore):
        super().__init__(task_store=task_store)

    async def build(
        self,
        params: MessageSendParams | None = None,
        task_id: str | None = None,
        context_id: str | None = None,
        task: Task | None = None,
        context: ServerCallContext | None = None,
    ) -> RequestContext:
        if context:
            headers = context.state.get("headers", {})
            token = headers.get("authorization", "").replace("Bearer ", "")
            if token:
                try:
                    claims = await validator.validate(token)
                    context.user = KAgentUser(user_id=claims["sub"])
                except Exception as e:
                    logger.error(f"Failed to validate token: {e}")
        request_context = await super().build(params, task_id, context_id, task, context)
        return request_context
