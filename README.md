# api-rate-limiter

A standalone rate-limiting/quota service that businesses drop into their existing API — as a middleware call or a reverse proxy — without changing their API contract.

Full MVP definition: [docs/MVP.md](docs/MVP.md). Week 1 ships the **Check** binary only.

## Who it's for

A small-to-mid SaaS team that has an API, is starting to hit abuse/overload problems or needs tiered rate limits (Free/Pro/Enterprise), and doesn't want to build or maintain Redis + Lua rate-limiting logic in-house.

## MVP goal

Prove one thing convincingly: **atomic, correct, low-latency rate limiting that survives concurrent load and degrades predictably when its dependency (Redis) fails.** Everything else in scope exists to make that demonstrable and integrable.

## Run Check (CORE)

Needs `protoc`, `protoc-gen-go`, and `protoc-gen-go-grpc` on `PATH`. Generated Go is local (`make proto`); it is not committed.

```bash
make redis

# another terminal
make proto
go run ./cmd/check -config configs/check.example.yaml
```

`Check` is h2c (plaintext HTTP/2) on `:50051` by default:

```bash
grpcurl -plaintext -d '{"key":"demo","cost":1}' 127.0.0.1:50051 check.v1.Checker/Check
```

Manual tests (no CI):

```bash
make test
make test-race
```

## Known spec

- Sliding-window counter (token bucket is a stretch goal)
- Redis + Lua for atomic check-and-increment
- gRPC `Check(key, cost) → (allowed, remaining, retry_after_ms)`
- Integration adapters: Go `net/http` middleware, Python FastAPI/Flask middleware, and an `httputil.ReverseProxy` mode
- Configurable fail-open vs fail-closed when Redis is unreachable
- Minimal live dashboard, load-test/demo tooling, Docker Compose, JSON logs, and Prometheus metrics
- Correctness tests for concurrency (exactly M of N allowed) and Redis-down behavior

## Out of scope

This is a signal, not an omission:

- Multi-region / geo-distributed rate limiting
- Billing/payment integration
- Full auth/RBAC for the dashboard
- Horizontal sharding of the rate-limit store (single Redis is fine for MVP)
- WebSocket/streaming-aware proxying beyond basic passthrough
