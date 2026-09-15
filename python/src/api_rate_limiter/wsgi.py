from __future__ import annotations

from collections.abc import Callable

from api_rate_limiter.decision import BAD_BODY, DENY_BODY, deny_headers, evaluate, parse_fail
from api_rate_limiter.key import KeyFn, KeyMissing, parse_key_source


class RateLimitWSGI:
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

    def __call__(self, environ, start_response):
        headers = _wsgi_headers(environ)
        ip = (environ.get("REMOTE_ADDR") or "").strip()
        try:
            key = self.key_fn(headers, ip)
        except KeyMissing:
            start_response("400 Bad Request", [("Content-Type", "application/json")])
            return [BAD_BODY]
        cost = self.cost_fn(headers) if self.cost_fn else self.cost
        decision = evaluate(self.stub, key, cost, self.fail)
        if decision.kind == "bad_request":
            start_response("400 Bad Request", [("Content-Type", "application/json")])
            return [BAD_BODY]
        if decision.kind == "deny":
            hdrs = [(k.decode(), v.decode()) for k, v in deny_headers(decision.remaining, decision.retry_after_ms)]
            start_response("429 Too Many Requests", hdrs)
            return [DENY_BODY]
        if decision.kind == "pass":
            return self.app(environ, start_response)

        def wrapped_start(status, resp_headers, exc_info=None):
            resp_headers = list(resp_headers) + [("X-RateLimit-Remaining", str(decision.remaining))]
            return start_response(status, resp_headers, exc_info)

        return self.app(environ, wrapped_start)


def _wsgi_headers(environ) -> dict[str, str]:
    headers = {}
    for k, v in environ.items():
        if k.startswith("HTTP_"):
            headers[k[5:].replace("_", "-").lower()] = v
    return headers
