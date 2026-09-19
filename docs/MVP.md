# api-rate-limiter — MVP Definition & Feature Scope

## One-line pitch
A standalone rate-limiting/quota service that businesses drop into their existing API — as a middleware call or a reverse proxy — without changing their API contract.

## Who it's for (target user in the pitch)
A small-to-mid SaaS team that has an API, is starting to hit abuse/overload problems or needs tiered rate limits (Free/Pro/Enterprise), and doesn't want to build/maintain their own Redis + Lua rate-limiting logic in-house.

## MVP goal
Prove one thing convincingly: **atomic, correct, low-latency rate limiting that survives concurrent load and degrades predictably when its dependency (Redis) fails.** Everything else in scope exists to make that demonstrable and integrable, not to add unrelated features.

---

## Must-have features (MVP)

### 1. Core rate-limit engine
- Algorithm: sliding window counter (simpler, defensible) — token bucket as a stretch goal if time allows
- Redis + Lua script for atomic check-and-increment (no race conditions under concurrent requests)
- Configurable per key: limit, window duration, fail-open vs fail-closed on Redis failure
- Key can be API key, IP, or a custom string the caller provides — engine doesn't care what it represents

### 2. gRPC check API
- Single primary RPC: `Check(key, cost) → (allowed, remaining, retry_after_ms)`
- `cost` lets a caller weight expensive requests higher (e.g., a search endpoint costs 5, a health check costs 0)
- Config RPC or config file to set limits per key-prefix/route (doesn't need a full admin UI for MVP — a YAML/JSON config file is fine)

### 3. Integration adapters (the "no contract change" story)
- Python middleware: FastAPI/Flask decorator or ASGI/WSGI middleware that calls the gRPC check before the request handler runs
- Go middleware: net/http middleware doing the same
- Reverse proxy mode: `httputil.ReverseProxy`-based proxy that checks the limit, then transparently forwards the request byte-for-byte to the origin if allowed
- One demo origin API (small FastAPI app) to rate-limit against in demos

### 4. Fail-open / fail-closed behavior
- Configurable per route/key what happens when Redis is unreachable
- This must be demonstrably correct, not just a config flag that does nothing — kill Redis mid-demo and show both behaviors

### 5. Minimal dashboard
- Live view: requests/sec, blocked vs allowed count, per-key usage
- A visible Redis-health indicator tied to the fail-open/fail-closed setting
- Doesn't need auth, multi-tenancy, or user accounts for MVP — single demo view is enough

### 6. Load-test / demo tooling
- A script that fires configurable req/sec against the demo API, so the dashboard has something to show live
- Scripted "kill Redis" step for the demo

### 7. Correctness tests
- Concurrency test: fire N simultaneous requests against a limit of M, assert exactly M are allowed (proves the Lua script is atomic — this is the test you'll point to in interviews)
- Fail-open/fail-closed behavior test with Redis actually down (e.g., via testcontainers or docker stop)

### 8. Docs and packaging
- README covering: the problem, Pattern A (proxy) vs Pattern B (middleware) explanation, architecture diagram, how to run locally
- Docker Compose: one command spins up Redis, api-rate-limiter, and the demo origin API
- Structured logs (JSON) + a couple of Prometheus metrics (requests_total, blocked_total, redis_up)

---

## Nice-to-have (only if time remains — do not let these delay the must-haves)
- Token bucket algorithm as a second strategy, selectable per key
- Admin RPC/UI to change limits at runtime instead of editing a config file
- Multi-tenant dashboard (per-tenant view, not just global)
- Basic API-key issuance flow (generate a key, tie it to a limit tier) — this is what makes it feel like a "product" rather than a library, so it's worth it if week 4 has slack
- Distributed/multi-node correctness test (multiple api-rate-limiter instances hitting the same Redis, confirming no over-admission)

## Explicitly out of scope (say this in the README — it's a signal, not an omission)
- Multi-region / geo-distributed rate limiting
- Billing/payment integration
- Full auth/RBAC for the dashboard
- Horizontal sharding of the rate-limit store (single Redis instance is fine for MVP; note in README what you'd do differently at 10x scale — e.g., Redis Cluster with consistent hashing on key)
- WebSocket/streaming-aware proxying beyond basic passthrough

---

## Success criteria (how you know the MVP is "done")
1. A viewer can watch the load-test script hit the demo API and see the dashboard throttle requests exactly at the configured limit, live.
2. Killing Redis mid-demo produces the configured fail-open or fail-closed behavior, visibly, within a couple seconds.
3. The concurrency test passes reliably (run it 10x, not just once — race conditions are flaky by nature).
4. Someone unfamiliar with the project can `docker compose up` and get the full demo running in under 5 minutes, per the README.
5. The README's Pattern A vs Pattern B section is clear enough that a non-expert could decide which integration mode fits their situation.

## Rough week mapping (unchanged from before, for reference)
- Week 1: Core engine + gRPC check API + concurrency tests
- Week 2: Middleware adapters (Python, Go) + reverse proxy mode + demo origin API
- Week 3: Dashboard + load-test tooling + fail-open/closed demo polish
- Week 4: Docs, Docker Compose, metrics/logging, README storytelling
