import os

from api_rate_limiter.asgi import RateLimitMiddleware
from api_rate_limiter.client import dial_aio

from app import app as origin


class Limited:
    def __init__(self):
        self.channel = None
        self.app = None

    async def __call__(self, scope, receive, send):
        if scope["type"] == "lifespan":
            async def hook():
                msg = await receive()
                if msg["type"] == "lifespan.startup":
                    self.channel, stub = dial_aio(os.environ.get("CHECK_ADDR", "127.0.0.1:50051"))
                    self.app = RateLimitMiddleware(
                        origin, stub, key="header:X-API-Key", cost=1, fail="closed"
                    )
                elif msg["type"] == "lifespan.shutdown" and self.channel is not None:
                    await self.channel.close()
                return msg

            await origin(scope, hook, send)
            return
        await self.app(scope, receive, send)


app = Limited()
