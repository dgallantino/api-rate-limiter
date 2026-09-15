# AGENT

Notes for coding agents working in this repo.

## Source of truth

1. Read [../MVP.md](../MVP.md) first.
2. Treat [../ROADMAP/README.md](../ROADMAP/README.md) as sequencing. Do not skip ahead unless asked.
3. Do not scaffold application code until asked.

## Layout

- `internal/` — private engine, Check service, and proxy YAML config.
- `pkg/httplimit` — exported Go `net/http` middleware that is a gRPC client of Check (not an in-process `internal/engine` import).
- `python/` — ASGI/WSGI adapters; generated stubs in gitignored `python/gen/`.
- `demo/origin` — FastAPI origin with no limiter of its own.
