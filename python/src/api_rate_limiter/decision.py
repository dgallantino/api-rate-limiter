from __future__ import annotations

import inspect
from dataclasses import dataclass

import grpc

from api_rate_limiter.client import make_request

DENY_BODY = b'{"error":"too_many_requests"}'
BAD_BODY = b'{"error":"bad_request"}'


def parse_fail(s: str | None) -> str:
    v = (s or "closed").strip().lower()
    if v in ("", "closed"):
        return "closed"
    if v == "open":
        return "open"
    raise ValueError("fail must be open or closed")


def retry_after_seconds(ms: int) -> int:
    if ms <= 0:
        return 0
    return (ms + 999) // 1000


def deny_headers(remaining: int, retry_after_ms: int) -> list[tuple[bytes, bytes]]:
    headers = [
        (b"content-type", b"application/json"),
        (b"x-ratelimit-remaining", str(remaining).encode()),
    ]
    sec = retry_after_seconds(retry_after_ms)
    if sec > 0:
        headers.append((b"retry-after", str(sec).encode()))
    return headers


@dataclass
class Decision:
    kind: str
    remaining: int = 0
    retry_after_ms: int = 0


def _is_invalid_argument(err: BaseException) -> bool:
    code = getattr(err, "code", None)
    if not callable(code):
        return False
    try:
        return code() == grpc.StatusCode.INVALID_ARGUMENT
    except Exception:
        return False


def _from_result(res: object) -> Decision:
    if not getattr(res, "allowed", False):
        return Decision(
            "deny",
            remaining=int(getattr(res, "remaining", 0) or 0),
            retry_after_ms=int(getattr(res, "retry_after_ms", 0) or 0),
        )
    return Decision("allow", remaining=int(getattr(res, "remaining", 0) or 0))


def _from_error(err: BaseException, fail: str) -> Decision:
    if _is_invalid_argument(err):
        return Decision("bad_request")
    if fail == "open":
        return Decision("pass")
    return Decision("deny", remaining=0, retry_after_ms=0)


def evaluate(stub, key: str, cost: int, fail: str) -> Decision:
    try:
        res = stub.Check(make_request(key, cost))
        if inspect.isawaitable(res):
            raise TypeError("async Check result on sync evaluate")
    except Exception as e:
        return _from_error(e, fail)
    return _from_result(res)


async def evaluate_async(stub, key: str, cost: int, fail: str) -> Decision:
    try:
        res = stub.Check(make_request(key, cost))
        if inspect.isawaitable(res):
            res = await res
    except Exception as e:
        return _from_error(e, fail)
    return _from_result(res)
