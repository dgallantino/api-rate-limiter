from __future__ import annotations

from collections.abc import Callable


class KeyMissing(Exception):
    pass


KeyFn = Callable[[dict[str, str], str], str]


def key_from_header(name: str) -> KeyFn:
    want = name.lower()

    def fn(headers: dict[str, str], _ip: str) -> str:
        v = (headers.get(want) or "").strip()
        if not v:
            raise KeyMissing(name)
        return v

    return fn


def key_from_ip() -> KeyFn:
    def fn(_headers: dict[str, str], ip: str) -> str:
        v = (ip or "").strip()
        if not v:
            raise KeyMissing("ip")
        return v

    return fn


def parse_key_source(spec: str | None) -> KeyFn:
    s = (spec or "").strip()
    if not s:
        return key_from_header("X-API-Key")
    if s.lower() == "ip":
        return key_from_ip()
    prefix = "header:"
    if s.lower().startswith(prefix):
        name = s[len(prefix) :].strip()
        if not name:
            raise ValueError("empty header name")
        return key_from_header(name)
    raise ValueError(f"key source {spec!r}: want header:<name> or ip")
