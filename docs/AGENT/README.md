# AGENT

Notes for coding agents working in this repo.

## Source of truth

1. Read [../MVP.md](../MVP.md) first.
2. Treat [../ROADMAP/README.md](../ROADMAP/README.md) as sequencing. Do not skip ahead unless asked.
3. Do not scaffold application code until asked.

## Layout

- `internal/` — private engine, Check service, proxy/dashboard YAML, in-process stats, Check ops HTTP (`/metrics`, `/healthz`).
- `pkg/httplimit` — exported Go `net/http` middleware that is a gRPC client of Check (not an in-process `internal/engine` import).
- `python/` — ASGI/WSGI adapters; generated stubs in gitignored `python/gen/`.
- `demo/origin` — FastAPI origin with no limiter of its own; `limited.py` is Pattern B (`GET /health` is not rate-limited).
- `cmd/check`, `cmd/proxy`, `cmd/dashboard`, `cmd/loadtest` — separate binaries; dashboard and adapters are gRPC clients of Check.
- Week 4 packaging: root `compose.yaml`, `deploy/` images, `configs/compose/` (service DNS). Proto is generated in the image build, not copied from `internal/gen/` / `python/gen/`.
