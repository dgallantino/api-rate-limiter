from __future__ import annotations

from collections.abc import Callable

from api_rate_limiter.decision import DENY_BODY, BAD_BODY, deny_headers, evaluate_async, parse_fail
from api_rate_limiter.key import KeyFn, KeyMissing, parse_key_source


class RateLimitMiddleware:
    def __init__(
        self,
        app,
        stub,
        *,
        key_fn: KeyFn | None = None,
        key: str | None = None,
        cost: int = 1,
        cost_fn: Callable[[dict[str, str]], int] | None = None,
        fail: str = "closed",
    ):
        self.app = app
        self.stub = stub
        self.key_fn = key_fn or parse_key_source(key)
        self.cost = cost
        self.cost_fn = cost_fn
        self.fail = parse_fail(fail)

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        headers = {
            k.decode("latin1").lower(): v.decode("latin1")
            for k, v in scope.get("headers", [])
        }
        ip = ""
        if scope.get("client"):
            ip = scope["client"][0] or ""
        try:
            key = self.key_fn(headers, ip)
        except KeyMissing:
            await _send_json(send, 400, BAD_BODY)
            return
        cost = self.cost_fn(headers) if self.cost_fn else self.cost
        decision = await evaluate_async(self.stub, key, cost, self.fail)
        if decision.kind == "bad_request":
            await _send_json(send, 400, BAD_BODY)
            return
        if decision.kind == "deny":
            await _send_json(
                send, 429, DENY_BODY, deny_headers(decision.remaining, decision.retry_after_ms)
            )
            return
        if decision.kind == "pass":
            await self.app(scope, receive, send)
            return

        remaining = str(decision.remaining).encode()

        async def send_wrapper(message):
            if message["type"] == "http.response.start":
                hdrs = list(message.get("headers", []))
                hdrs.append((b"x-ratelimit-remaining", remaining))
                message = {**message, "headers": hdrs}
            await send(message)

        await self.app(scope, receive, send_wrapper)


async def _send_json(send, status: int, body: bytes, extra: list[tuple[bytes, bytes]] | None = None):
    headers = [(b"content-type", b"application/json"), (b"content-length", str(len(body)).encode())]
    if extra:
        headers.extend(extra)
    await send({"type": "http.response.start", "status": status, "headers": headers})
    await send({"type": "http.response.body", "body": body})
