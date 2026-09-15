from __future__ import annotations

from types import SimpleNamespace

import grpc
import pytest
from flask import Flask
from starlette.applications import Starlette
from starlette.responses import PlainTextResponse
from starlette.routing import Route
from starlette.testclient import TestClient

from api_rate_limiter.asgi import RateLimitMiddleware
from api_rate_limiter.decision import DENY_BODY
from api_rate_limiter.wsgi import RateLimitWSGI


class FakeStub:
    def __init__(self, fn):
        self.fn = fn

    def Check(self, req):
        return self.fn(req)


class FakeRpcError(Exception):
    def __init__(self, code):
        self._code = code

    def code(self):
        return self._code


def _asgi(stub, **kw) -> TestClient:
    async def ok(_request):
        return PlainTextResponse("", status_code=204)

    app = Starlette(routes=[Route("/", ok)])
    kw.setdefault("cost", 1)
    return TestClient(RateLimitMiddleware(app, stub, **kw))


def _wsgi(stub, **kw):
    app = Flask(__name__)

    @app.get("/")
    def ok():
        return "", 204

    kw.setdefault("cost", 1)
    app.wsgi_app = RateLimitWSGI(app.wsgi_app, stub, **kw)
    return app.test_client()


@pytest.mark.parametrize("kind", ["asgi", "wsgi"])
def test_allow(kind):
    stub = FakeStub(lambda req: SimpleNamespace(allowed=True, remaining=7, retry_after_ms=0))
    if kind == "asgi":
        rec = _asgi(stub).get("/", headers={"X-API-Key": "free:demo"})
    else:
        rec = _wsgi(stub).get("/", headers={"X-API-Key": "free:demo"})
    assert rec.status_code == 204
    assert rec.headers["X-RateLimit-Remaining"] == "7"


@pytest.mark.parametrize("kind", ["asgi", "wsgi"])
def test_deny(kind):
    stub = FakeStub(lambda req: SimpleNamespace(allowed=False, remaining=0, retry_after_ms=1500))
    if kind == "asgi":
        rec = _asgi(stub).get("/", headers={"X-API-Key": "k"})
    else:
        rec = _wsgi(stub).get("/", headers={"X-API-Key": "k"})
    assert rec.status_code == 429
    assert rec.headers["Retry-After"] == "2"
    assert rec.headers["X-RateLimit-Remaining"] == "0"
    body = rec.data if kind == "wsgi" else rec.content
    assert body == DENY_BODY


@pytest.mark.parametrize("kind", ["asgi", "wsgi"])
def test_missing_key(kind):
    def boom(_req):
        raise AssertionError("check must not run")

    stub = FakeStub(boom)
    rec = _asgi(stub).get("/") if kind == "asgi" else _wsgi(stub).get("/")
    assert rec.status_code == 400


@pytest.mark.parametrize("kind", ["asgi", "wsgi"])
def test_fail_closed(kind):
    stub = FakeStub(lambda _req: (_ for _ in ()).throw(FakeRpcError(grpc.StatusCode.UNAVAILABLE)))
    rec = (
        _asgi(stub, fail="closed").get("/", headers={"X-API-Key": "k"})
        if kind == "asgi"
        else _wsgi(stub, fail="closed").get("/", headers={"X-API-Key": "k"})
    )
    assert rec.status_code == 429
    assert rec.headers.get("Retry-After") in (None, "")


@pytest.mark.parametrize("kind", ["asgi", "wsgi"])
def test_fail_open(kind):
    stub = FakeStub(lambda _req: (_ for _ in ()).throw(FakeRpcError(grpc.StatusCode.UNAVAILABLE)))
    rec = (
        _asgi(stub, fail="open").get("/", headers={"X-API-Key": "k"})
        if kind == "asgi"
        else _wsgi(stub, fail="open").get("/", headers={"X-API-Key": "k"})
    )
    assert rec.status_code == 204


@pytest.mark.parametrize("kind", ["asgi", "wsgi"])
def test_invalid_argument(kind):
    stub = FakeStub(lambda _req: (_ for _ in ()).throw(FakeRpcError(grpc.StatusCode.INVALID_ARGUMENT)))
    rec = (
        _asgi(stub).get("/", headers={"X-API-Key": "k"})
        if kind == "asgi"
        else _wsgi(stub).get("/", headers={"X-API-Key": "k"})
    )
    assert rec.status_code == 400


def test_cost_peek_forwarded():
    got = {}

    def fn(req):
        got["cost"] = req.cost
        return SimpleNamespace(allowed=True, remaining=3, retry_after_ms=0)

    rec = _asgi(FakeStub(fn), cost=0).get("/", headers={"X-API-Key": "k"})
    assert rec.status_code == 204
    assert got["cost"] == 0
