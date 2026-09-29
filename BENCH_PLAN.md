# Benchmark plan (throwaway)

> **Throwaway document.** It exists only to plan the benchmark work and gets deleted once that work is done. Do not link to it from code, docs, commits, or PRs.

> **Source of truth.** While the benchmark work is in progress, this document is the source of truth for scope, structure, formats, and PR order. If the implementation has to deviate, update this document in the same PR. Update each PR's **Status** as work lands.

Goal: turn "atomic, low-latency limits that survive concurrent load and fail open or closed when Redis is down" (the README tagline) into **numbers you can defend**, produced by **one command** (`make bench`), and presentable in a portfolio/CV.

---

## 0. Decisions already made

| Topic | Decision |
| --- | --- |
| Where the harness lives | **This repo**, under `bench/`, as a nested Go module. |
| Where it runs | **Dockerized, single machine** (this Fedora laptop). See §5. |
| Runner | A Go program at `bench/cmd/bench`, driven by `make bench`. |
| Load generator | **vegeta v12.13.0** (CLI) for both the bench and the demo. **`cmd/loadtest` is removed in the same PR that introduces vegeta** (PR 2). |
| Server-side latency | **No server-side latency histogram.** The "three Prometheus series only" rule stays. All latency is measured client-side by vegeta. |
| Report format | **Markdown + PNG charts.** |
| Tests | Root: `make test` (unchanged). Bench: `cd bench && go test ./...`, kept separate, **must not need Docker**. |
| Branching | `dev/bench/<topic>` branches, merged into **`dev/bench/staging`**, never `main`. |
| CI | **No GitHub Actions until the harness is stable** (milestone 6). |
| PR size | **< 500 changed lines per PR** (insertions + deletions), documentation (`*.md`) excluded. |
| Scope | Measurement groups A–E (§3). Efficiency/footprint and engineering-quality metrics are **out of scope**. |

---

## 1. Working agreement (for coding agents)

**Before starting a PR**
- Read §0–§8 and the PR's block in §9.
- Confirm every "Depends on" PR is merged into `dev/bench/staging`, then branch from the latest `dev/bench/staging`.
- `dev/bench/staging` doesn't exist yet. Before PR 1: create it from `main`, commit this file on it, and push it.
- Before PR 1 (and whenever the machine changes): run `lscpu -e` and confirm the CPU topology matches the defaults in §5. If it doesn't, stop and ask.

**Branches and PRs**
- Branch name: `dev/bench/<topic>`, using the topic given in the PR block (e.g. `dev/bench/skeleton`).
- Open with `gh pr create --base dev/bench/staging`. Never target or push to `main`. Don't merge; the owner reviews.
- One PR per plan item. Don't fold items together. If an item won't fit in 500 lines, split it and update this plan in the same PR.
- Size check (must be < 500): `git diff --shortstat origin/dev/bench/staging...HEAD -- . ':(exclude)*.md'`

**Commits**
- Match the repo history: one sentence, capitalized, ending with a period, saying what changed and why.
  Example from history: "Skip Redis while it is latched down so Checks do not wait on a dead store."

**Before opening the PR**
- `make test` passes.
- `cd bench && go test ./...` passes (from PR 1 on).
- Every "Done when" item in the PR block is checked. The PR description lists each item and how it was verified.
- This plan's Status for the PR is updated in the same PR (`todo` → `in review`). The owner sets `merged`.

**When the work is done:** delete this file in the PR that merges `dev/bench/staging` into `main`.

---

## 2. Where the repo stands today

| Already have | Gap for objective measurement |
| --- | --- |
| `cmd/loadtest`: constant-rate, open-model HTTP load | Records only allow / 429 / error counts; no latency, no machine-readable output. **Replaced by vegeta in PR 2.** |
| Check `/metrics`: `requests_total`, `blocked_total`, `redis_up` | No latency histogram, **and none will be added**. Still useful for `redis_up` timing and request counts. |
| Compose stack with healthchecks, Prometheus at 1s scrape | Demo configs (`free:` 20/min) are tuned for *showing* throttling, not for *stress testing*. The dashboard polls every 1s, which adds noise. Configs are baked into images with Compose service DNS names. |
| `concurrency_test.go`: N concurrent → exactly M admitted (miniredis) | Correctness proven in unit tests, **not under real Redis + real network + real load**. |
| GoReleaser builds prebuilt binaries on tags | Releases only ship `check`, `proxy`, `dashboard`. `origin` isn't released, so the bench builds from source via `deploy/Dockerfile`. |
| Origin `/work` returns instantly | Can't show "rate limiting protects origin work" because origin does no work. |

---

## 3. What to measure and why

Rows marked ⭐ are the strongest for a CV.

### A. Overhead: "What does the limiter cost me?"
Always report as a **difference from a baseline** (origin with no limiter).

- ⭐ **Added latency p50 / p95 / p99 / p99.9** for: baseline (origin direct) vs Pattern A (proxy) vs Pattern B (middleware).
- **Pattern A vs Pattern B cost**. The README argues B saves a network hop; this measurement proves it.

### B. Capacity: "How far does it scale?"
- ⭐ **Max sustainable throughput at an SLO** (e.g. highest RPS where p99 < 5 ms and errors < 0.1%).
- **Latency vs. throughput curve**: step the rate up and find the knee.
- **Where the bottleneck is** at the knee: Check CPU, Redis CPU, proxy, or the load generator.

### C. Correctness under load: "Is the limit actually enforced?"
- ⭐ **Over-admission rate**: admitted ÷ limit per key per window under high concurrency against *real* Redis. Target: 0%.
- **Sliding-window approximation error** at window boundaries (burst-at-boundary scenario).

### D. Resilience: "What happens when things break?"
- ⭐ **Failover detection time**: Redis killed → behaviour switches to fail-open or fail-closed. The docs claim "within about a second"; measure it.
- **Recovery time**: Redis back → normal limiting resumes.
- **Error and latency during the outage**: the latch-down design should make latency *drop*, because Check skips Redis.
- **Requests misclassified during the transition** (e.g. `pro:` briefly allowed, `free:` briefly denied).

### E. Protection value: "Does it actually help the business?"
- ⭐ **Origin shielding**: under abusive traffic at 10× the limit, origin receives about a limit's worth of requests, and origin latency stays flat.
- ⭐ **Noisy-neighbour isolation**: one abusive key doesn't degrade p99 for a well-behaved key.

---

## 4. Good-practice standards

1. **Percentiles, never averages.** p50/p95/p99/p99.9 and max. `vegeta report` stops at p99, so the runner computes percentiles itself (nearest-rank method) from vegeta's raw per-request results.
2. **Open-model load for latency.** vegeta's constant-rate attack doesn't wait for responses; it adds workers to hold the rate. Don't cap `-max-workers` in latency runs, or it turns into a closed model and under-reports tails (coordinated omission).
3. **Baseline + delta.** Every number sits next to the unlimited-origin control.
4. **Warm-up, then a steady-state window.** vegeta has no warm-up flag, so the runner drops the first N seconds by per-request timestamp.
5. **Repeat and show variance.** 3–5 runs per scenario; report median and spread.
6. **Isolate load generator from system under test.** On one machine this means CPU pinning (§5).
7. **Capture the environment with every result** (full list in PR 3).
8. **Define capacity against an SLO** (golden signals, USE for resources, RED for services).
9. **Config as code, results as artifacts.** Scenarios live in the repo; raw results sit next to the rendered report.
10. **Be honest about scope.** "Single host, Docker, loopback network" goes in every report header.

---

## 5. Single-machine Docker setup

Reference machine: AMD Ryzen 5 7640HS, 6 cores / 12 threads, Fedora, Docker and Podman both installed. The runtime is chosen the same way the root Makefile's `COMPOSE` variable does it (Docker if the daemon is up, else Podman) and recorded in metadata.

### Separate bench stack, not an overlay of the demo
`bench/compose.bench.yaml` is a **standalone** Compose file that reuses the `deploy/Dockerfile` targets (build context `..`). It runs under its own Compose project name, **`api-rate-limiter-bench`** (`-p`), so it never touches the demo's containers.

- **Services:** `redis`, `check`, `origin`, `origin-limited`, `proxy`, `prometheus`, `vegeta`. **No dashboard.**
- **Host networking (`network_mode: host`) on every service.** Every address is `127.0.0.1:<port>`.
- Bench configs are **mounted** from `bench/configs/`, overriding the configs baked into the images. The bench doesn't use `SetLimit`, so mounts are read-only.
- Healthchecks are defined in `compose.bench.yaml` against the bench ports.
- Pre-flight refuses to run if any bench port is in use or if the demo Compose project has running containers (they would compete for CPU).

### Ports (bench only; none are used elsewhere in the repo)

| Service | Port | How it's set |
| --- | --- | --- |
| redis | 16379 | `redis-server --port 16379` |
| check gRPC | 15051 | `listen_addr` in `bench/configs/check.*.yaml` |
| check `/metrics`, `/healthz` | 12112 | `metrics_addr` in `bench/configs/check.*.yaml` |
| origin | 18000 | `origin -addr :18000` |
| origin-limited | 18001 | `origin -limited -addr :18001`, `CHECK_ADDR=127.0.0.1:15051` |
| proxy | 18090 | `listen_addr` in `bench/configs/proxy.yaml` |
| prometheus | 19090 | `--web.listen-address=:19090` |

### CPU pinning: environment variables with defaults
`cpuset` on each service comes from an environment variable with a default, via Compose interpolation (`cpuset: "${BENCH_CPUS_SUT:-2,3,4,8,9,10}"`).

On the reference machine, logical CPU *n* and *n+6* are hyperthread siblings on physical core *n* (`lscpu -e`). The defaults pin whole physical cores:

| Variable | Default | Physical cores | Services |
| --- | --- | --- | --- |
| `BENCH_CPUS_LOADGEN` | `0,1,6,7` | 0, 1 | vegeta |
| `BENCH_CPUS_SUT` | `2,3,4,8,9,10` | 2, 3, 4 | check, proxy, origin, origin-limited |
| `BENCH_CPUS_REDIS` | `5,11` | 5 | redis, prometheus |

The agent checks `lscpu -e` before relying on these defaults (§1).

### Other runner environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `BENCH_ONLY` | *(all)* | Glob of scenario names to run, e.g. `overhead-*` |
| `BENCH_COMPOSE` | set by `make bench` from the Makefile's `COMPOSE` | Compose command to use |

### Report disclosure
Every report header states: single host, loopback network, host networking, container runtime + version, vegeta version, CPU pinning layout.

---

## 6. `bench/` structure

```
bench/
  go.mod                     module github.com/dgallantino/api-rate-limiter/bench, go 1.26.5
  cmd/bench/main.go          runner entry point
  internal/
    compose/                 compose up/down/stop/start/run behind an interface
    preflight/               port checks, demo-stack check, runtime detection
    metadata/                environment capture
    scenario/                YAML loader + validation (§7)
    vegeta/                  targets generation, attack invocation, result parsing
    analysis/                percentiles, warm-up drop, repeats aggregation, deltas, SLO, timelines
    resources/               container CPU/mem sampling (PR 8)
    report/                  report.md rendering
    charts/                  PNG rendering (gonum/plot)
  scenarios/                 one YAML file per scenario (§7)
  compose.bench.yaml         standalone stack (§5)
  configs/
    check.capacity.yaml        high limits: measure the limiter path, not 429s
    check.correctness.yaml     tight limits, short windows: over-admission, isolation, failover
    proxy.yaml
    prometheus.yml
  report/
    report.md.tmpl           Markdown template (text/template)
  results/                   gitignored and .dockerignored
Makefile (root)              bench-up, bench-down, bench
```

### Results layout (one directory per `make bench`)

```
bench/results/<YYYYMMDD-HHMMSS>-<short-sha>[-dirty]/
  metadata.json
  targets/<scenario>.txt                        generated vegeta targets
  raw/<scenario>/run-<n>.jsonl                  vegeta encode --to json (one line per request)
  raw/<scenario>/run-<n>-step-<rate>.jsonl      ramp scenarios
  raw/<scenario>/resources-run-<n>.jsonl        PR 8
  raw/<scenario>/redis-up-run-<n>.jsonl         PR 13
  charts/*.png
  report.md
```

### Conventions
- The nested module does **not** import the main module. It talks to the stack over HTTP and the container runtime, so no `replace` directive and no `go.work`.
- Because `bench/` has its own `go.mod`, the root `go test ./...` skips it automatically. The two test suites stay separate.
- vegeta runs as a **CLI in its own pinned container**, not as a library inside the runner, so load generation stays on its own cores. The runner runs on the host and does analysis after each attack, never during one.
- The vegeta container mounts `bench/results/` at `/results`; the runner writes the targets file there and vegeta writes raw results there.
- Raw results are never committed.

### Testing (`cd bench && go test ./...`)
- **Must not need Docker, network, or a running stack.**
- Everything that shells out (compose, vegeta, stats) sits behind a small interface; tests use fakes.
- Parsing and analysis are tested against fixture files in `testdata/` (recorded vegeta JSON lines, recorded `docker stats` / `podman stats` output).
- Anything that needs real containers is verified through the PR's "Done when" steps, not `go test`.

---

## 7. Scenario format

One file per scenario: `bench/scenarios/<name>.yaml`. The filename stem must equal `name`. Decoding is **strict**: unknown fields are an error. Fields whose PR hasn't landed yet are rejected with "not implemented until PR N".

### Schema

```yaml
name: overhead-pattern-a        # required; [a-z0-9-]+; equals filename stem
description: Pattern A at a fixed rate   # required; one line; shown in the report
check_config: capacity          # required; capacity | correctness → bench/configs/check.<value>.yaml
target:
  service: proxy                # required; origin | origin-limited | proxy (mapped to 127.0.0.1:<port> from §5)
  path: /work                   # default /work
  method: GET                   # default GET
header: X-API-Key               # default X-API-Key
keys:                           # required; at least one
  - name: pro                   # required; [a-z0-9-]+; label used in results and report
    value: "pro:bench"          # required; header value sent
    rate: 200/s                 # optional (PR 16); all keys or none
repeats: 3                      # default 3; >= 1
warmup: 5s                      # default 5s; dropped from analysis by timestamp; must be < duration

load:
  type: constant                # required; constant | ramp (PR 7) | burst (PR 10)

  # type: constant
  rate: 2000/s                  # vegeta rate syntax; required unless keys carry rate
  duration: 30s                 # required; includes warmup

  # type: ramp (PR 7). Each step is its own attack and its own warm-up.
  start: 1000/s
  step: 1000/s
  max: 20000/s
  step_duration: 20s
  stop_on_slo_breach: true      # default true

  # type: burst (PR 10). vegeta -rate=0 -max-workers=<workers>.
  workers: 64
  duration: 3s

slo:                            # optional; required for ramp
  p99: 5ms
  error_rate: 0.001             # errors = responses that are neither 2xx nor 429, plus transport errors

timeline:                       # optional (PR 12)
  bucket: 100ms

actions:                        # optional (PR 13); constant only
  - at: 20s                     # offset from attack start (warm-up included)
    do: redis_stop              # redis_stop | redis_start
```

### Validation rules
- Type-specific `load` fields are only allowed with their type.
- `keys[].rate` is all-or-none. If keys carry rates, `load.rate` must be absent, and the runner runs one parallel attack per key (tagged with vegeta `-name`).
- Without per-key rates, the runner runs one attack at `load.rate` and vegeta round-robins over the keys.
- `actions` are only valid with `type: constant`; every `at` must be < `duration`.
- `ramp` requires `slo`, and `max` must be ≥ `start`.

### Runner behaviour per scenario
- Before the scenario: recreate `check` with the scenario's `check_config` if it differs from the running one, and wait healthy.
- Before every repeat: `FLUSHALL` Redis so counters don't carry over.
- If a `capacity` scenario sees any 429s, the report flags the scenario as invalid (limits were too low to measure the limiter path).

### Examples

```yaml
# bench/scenarios/smoke.yaml
name: smoke
description: Short Pattern A run to prove the pipeline works
check_config: capacity
target: { service: proxy }
keys:
  - { name: pro, value: "pro:bench" }
repeats: 1
warmup: 2s
load: { type: constant, rate: 100/s, duration: 10s }
```

```yaml
# bench/scenarios/failover-redis-kill.yaml
name: failover-redis-kill
description: Redis stopped mid-run; free fails open, pro fails closed
check_config: correctness
target: { service: proxy }
keys:
  - { name: free, value: "free:bench" }
  - { name: pro,  value: "pro:bench" }
repeats: 3
warmup: 5s
load: { type: constant, rate: 500/s, duration: 60s }
timeline: { bucket: 100ms }
actions:
  - { at: 20s, do: redis_stop }
  - { at: 40s, do: redis_start }
```

```yaml
# bench/scenarios/noisy-neighbour.yaml
name: noisy-neighbour
description: One key floods at 10x quota while a polite key stays within quota
check_config: correctness
target: { service: proxy }
keys:
  - { name: abusive, value: "free:abusive", rate: 1000/s }
  - { name: polite,  value: "pro:polite",   rate: 50/s }
repeats: 3
warmup: 5s
load: { type: constant, duration: 30s }
```

---

## 8. Automation: one trigger → report

```
make bench        (cd bench && BENCH_COMPOSE="$(COMPOSE)" go run ./cmd/bench)
   │
   ├─ 1. Pre-flight          ── bench ports free, demo project not running, runtime detected
   ├─ 2. Provision           ── build from current commit; compose -p api-rate-limiter-bench -f compose.bench.yaml up
   ├─ 3. Wait healthy
   ├─ 4. Capture metadata    ── metadata.json
   ├─ 5. Run scenarios       ── per scenario (BENCH_ONLY filter): configure → per repeat: flush → attack → store raw
   ├─ 6. Collect             ── vegeta raw results, Prometheus range queries, container CPU/mem, redis_up samples
   ├─ 7. Teardown            ── compose down, always (even on failure or Ctrl-C)
   └─ 8. Report              ── analysis → report.md + charts/*.png
```

Runner design principles:
- **Idempotent and self-cleaning.** A failed run leaves no containers behind.
- **Scenarios are data, not code.** Adding a scenario means adding a YAML file.
- **The report is generated, never hand-edited.**

---

## 9. PR plan

Line estimates are rough. Status values: `todo`, `in progress`, `in review`, `merged`.

### Milestone 1: foundation
Done when `make bench` runs `smoke` end-to-end and produces a report.

#### PR 1: bench skeleton
Branch `dev/bench/skeleton` · ~150–250 lines · Depends on: none · **Status: todo**

Scope:
- `bench/go.mod`; `bench/cmd/bench/main.go` stub that prints usage.
- `bench/compose.bench.yaml`: all services from §5 except `vegeta`, host networking, bench ports, cpusets from env vars with defaults, bench healthchecks, read-only config mounts.
- `bench/configs/check.capacity.yaml`, `check.correctness.yaml`, `proxy.yaml`, `prometheus.yml` on 127.0.0.1 and bench ports.
- Root `Makefile`: `bench-up`, `bench-down` (project `api-rate-limiter-bench`, using the existing `COMPOSE` variable).
- `.gitignore` and `.dockerignore`: add `bench/results/`.

Done when:
- [ ] `make bench-up` starts every service and all report healthy.
- [ ] `curl -H 'X-API-Key: pro:bench'` returns 200 on `127.0.0.1:18000/work`, `:18001/work`, and `:18090/work`.
- [ ] `curl 127.0.0.1:12112/metrics` shows the three series; Prometheus UI answers on `:19090`.
- [ ] Container inspect shows the default cpusets; setting `BENCH_CPUS_SUT=2,8` before `make bench-up` changes them.
- [ ] `make bench-down` leaves no containers in project `api-rate-limiter-bench`.
- [ ] Demo `make compose-up` still works; `make test` passes; `cd bench && go build ./...` succeeds.

#### PR 2: vegeta replaces `cmd/loadtest`
Branch `dev/bench/vegeta` · ~250–350 lines (~190 of them deletions) · Depends on: PR 1 · **Status: todo**

Scope (all in this one PR):
- `deploy/Dockerfile`: replace the `loadtest` target with a `vegeta` target; `ARG VEGETA_VERSION=v12.13.0`, installed via `go install github.com/tsenart/vegeta/v12@${VEGETA_VERSION}`.
- Demo `compose.yaml`: keep the service name `loadtest` and profile `load` (so the README command and `make compose-loadtest` don't change), build target `vegeta`, command `vegeta attack … | vegeta report` at 20/s for 30s against `http://proxy:8080/work`, with a targets file `configs/compose/vegeta-targets.txt` rotating `free:demo` / `pro:demo`.
- Root `Makefile`: `VEGETA_VERSION := v12.13.0`; `loadtest` runs `go run github.com/tsenart/vegeta/v12@$(VEGETA_VERSION)` against `127.0.0.1:8080` with `configs/vegeta-targets.example.txt`; drop `cmd/loadtest` from `build`.
- Delete `cmd/loadtest/`.
- `bench/compose.bench.yaml`: add the `vegeta` service (host network, `BENCH_CPUS_LOADGEN`, `bench/results` mounted at `/results`).
- README and ARCHITECTURE: replace `cmd/loadtest` mentions (docs, not counted).
- Accepted behaviour change: `vegeta report` prints totals and status-code counts, not the old per-key split. The dashboard still shows per-key usage.

Done when:
- [ ] `rg 'cmd/loadtest'` finds nothing outside this plan.
- [ ] Demo: `make compose-up` then `make compose-loadtest` prints a vegeta report with both 200 and 429 status codes.
- [ ] `make loadtest` works against the host Makefile loop.
- [ ] The Dockerfile `ARG` and the Makefile `VEGETA_VERSION` both say `v12.13.0`.
- [ ] Bench: the `vegeta` service runs with the `BENCH_CPUS_LOADGEN` cpuset and can write to `/results`.
- [ ] `make build` and `make test` pass.

#### PR 3: runner lifecycle
Branch `dev/bench/runner-lifecycle` · ~300–400 lines · Depends on: PR 1 · **Status: todo**

Scope:
- `internal/compose`, `internal/preflight`, `internal/metadata`; `cmd/bench` wires them together.
- `make bench` target (`cd bench && BENCH_COMPOSE="$(COMPOSE)" go run ./cmd/bench`).
- Pre-flight: bench ports free, demo project not running.
- Guaranteed teardown on success, error, and SIGINT/SIGTERM.
- Creates the results directory (§6) and writes `metadata.json`: commit SHA + dirty flag, timestamp, Go version from `bench/go.mod` and the Dockerfile base image, vegeta version from the Dockerfile `ARG`, CPU model, logical CPU count, kernel, OS release, container runtime + version, compose version, cpuset values, SHA-256 of each config and scenario file.

Done when:
- [ ] `cd bench && go test ./...` covers pre-flight, metadata, and teardown ordering using fakes (no Docker).
- [ ] With no scenarios, `make bench` brings the stack up, waits healthy, writes `metadata.json`, tears down, and exits 0.
- [ ] Ctrl-C during `make bench` leaves no bench containers.
- [ ] Pre-flight fails with a clear message when a bench port is taken, and when the demo stack is running.

#### PR 4: scenario loader + vegeta driver
Branch `dev/bench/scenarios` · ~300–400 lines · Depends on: PR 2, PR 3 · **Status: todo**

Scope:
- `internal/scenario`: strict loader for the base fields and `type: constant` (§7); other types/fields rejected as "not implemented until PR N".
- `internal/vegeta`: generate the targets file, run one attack per repeat in the `vegeta` container, write `raw/<scenario>/run-<n>.jsonl`.
- Runner behaviour from §7: `check_config` switch, `FLUSHALL` before every repeat, `BENCH_ONLY` filter.
- `bench/scenarios/smoke.yaml`.

Done when:
- [ ] Loader tests cover valid files, unknown fields, missing required fields, and not-yet-implemented fields.
- [ ] Targets generation has a golden-file test.
- [ ] `make bench` runs `smoke` and writes one raw file per repeat; requests come from the vegeta container.
- [ ] `BENCH_ONLY=nothing make bench` runs no scenarios and still exits cleanly.

#### PR 5: report v1
Branch `dev/bench/report` · ~300–400 lines · Depends on: PR 4 · **Status: todo**

Scope:
- `internal/analysis`: parse raw results, drop warm-up by timestamp, nearest-rank p50/p95/p99/p99.9/max, status-code counts, error rate, median + min/max across repeats.
- `internal/report` + `report/report.md.tmpl`: environment header (from `metadata.json`, including the §5 disclosure) and one table per scenario.

Done when:
- [ ] Analysis tests run against fixture raw files with hand-checked expected percentiles and warm-up cut-off.
- [ ] `make bench` produces `report.md` with the environment header and the `smoke` table. **Milestone 1 complete.**

### Milestone 2: overhead and capacity (A, B)

#### PR 6: overhead scenarios
Branch `dev/bench/overhead` · ~150–250 lines · Depends on: PR 5 · **Status: todo**

Scope: `overhead-baseline` (origin), `overhead-pattern-a` (proxy), `overhead-pattern-b` (origin-limited), all on `capacity` at the same rate; report section with per-percentile delta vs baseline.

Done when:
- [ ] Delta computation has unit tests.
- [ ] The report shows an Overhead table (baseline, A, B, and deltas) and flags any scenario that saw 429s.

#### PR 7: rate ramp + SLO
Branch `dev/bench/ramp` · ~250–350 lines · Depends on: PR 5 · **Status: todo**

Scope: `type: ramp` and `slo`; each step is its own constant-rate attack; stop on SLO breach; ramp scenarios for baseline, A, and B.

Done when:
- [ ] Tests cover step generation, SLO evaluation, and stop-on-breach.
- [ ] The report shows a per-step table and "max rate meeting SLO" per target.

#### PR 8: resource sampling
Branch `dev/bench/resources` · ~200–300 lines · Depends on: PR 3 · **Status: todo**

Scope: sample CPU% and memory per container at a fixed interval during steady state (Docker and Podman `stats` output); per-scenario (and per ramp step) resource table; flag load-generator saturation (vegeta CPU near its cpuset capacity) as a warning.

Done when:
- [ ] Parser tests against recorded Docker and Podman `stats` fixtures.
- [ ] The report shows a resource table and the saturation warning when triggered.

#### PR 9: PNG charts
Branch `dev/bench/charts` · ~250–400 lines · Depends on: PR 7 · **Status: todo**

Scope: `internal/charts` with gonum/plot; latency vs throughput chart (p50 and p99 for baseline, A, B) from ramp results; embedded in `report.md` by relative path.

Done when:
- [ ] A test renders a chart from fixture data and decodes the output as a valid, non-empty PNG.
- [ ] `report.md` shows the chart.

### Milestone 3: correctness (C)

#### PR 10: over-admission
Branch `dev/bench/over-admission` · ~250–350 lines · Depends on: PR 5 · **Status: todo**

Scope: `type: burst` (`-rate=0 -max-workers=<workers>`); short windows and small limits in `check.correctness.yaml`; admitted ÷ limit per key per window across repeats; `over-admission` scenario.

Done when:
- [ ] Tests cover admitted-per-window counting against fixtures.
- [ ] The report shows an over-admission table with the 0% target and the total requests sent.

#### PR 11: window-boundary error
Branch `dev/bench/window-boundary` · ~250–350 lines · Depends on: PR 10 · **Status: todo**

Scope: schedule a burst just before and just after a window boundary (windows are epoch-aligned: `ws = now - now % window`); compare observed admits with an ideal sliding log computed from request timestamps.

Done when:
- [ ] Ideal-sliding-log calculation has unit tests.
- [ ] The report shows observed vs ideal admits and the difference.

### Milestone 4: resilience (D)

#### PR 12: timeline bucketing
Branch `dev/bench/timeline` · ~150–250 lines · Depends on: PR 5 · **Status: todo**

Scope: `timeline.bucket`; group per-request results into fixed buckets (count per status code and p99 latency per key per bucket).

Done when:
- [ ] Bucketing tests against fixtures, including empty buckets.

#### PR 13: Redis stop/start actions
Branch `dev/bench/actions` · ~250–350 lines · Depends on: PR 4 · **Status: todo**

Scope: `actions` (`redis_stop`, `redis_start`) executed by the runner at their offsets during the attack; sample `redis_up` from `127.0.0.1:12112/metrics` every 100 ms into `redis-up-run-<n>.jsonl`; Redis is always restarted before teardown.

Done when:
- [ ] Tests with a fake clock and fake compose verify action timing and ordering.
- [ ] `failover-redis-kill` runs end-to-end, and Redis is running again afterwards.

#### PR 14: failover analysis
Branch `dev/bench/failover` · ~300–400 lines · Depends on: PR 12, PR 13, PR 9 · **Status: todo**

Scope: detection time (Redis stop → responses match the policy fail mode), recovery time (Redis start → normal limiting), misclassified requests during both transitions, latency during the outage; timeline PNG.

Done when:
- [ ] Tests against fixture timelines with known transition points.
- [ ] The report shows a failover section and the timeline chart.

### Milestone 5: protection value (E)

#### PR 15: origin simulated cost
Branch `dev/bench/origin-cost` · ~100–200 lines · Depends on: none · **Status: todo**

Scope: `cmd/origin -work-delay <duration>` (default 0, so the demo is unchanged); `GET /health` also returns `work_count` (number of `/work` requests that ran). `/health` already skips Check, so no new skip rule is needed. Bench compose sets a non-zero delay.

Done when:
- [ ] `cmd/origin` tests cover the delay and the counter, including that denied requests don't increment it.
- [ ] `make test` passes; demo behaviour is unchanged with the default.

#### PR 16: per-key rates
Branch `dev/bench/per-key-rates` · ~150–250 lines · Depends on: PR 4 · **Status: todo**

Scope: `keys[].rate`; one parallel vegeta attack per key tagged with `-name`; results merged and analysed per key.

Done when:
- [ ] Loader tests cover the all-or-none rule.
- [ ] The report splits results by key name for a per-key-rate scenario.

#### PR 17: shielding + noisy neighbour
Branch `dev/bench/protection` · ~200–300 lines · Depends on: PR 15, PR 16, PR 5 · **Status: todo**

Scope: `origin-shielding` (abusive key at 10× quota; compare `work_count` delta with the limit) and `noisy-neighbour` (polite key's p99 with and without the abusive key); report sections.

Done when:
- [ ] Report shows origin requests vs limit, and the polite key's p99 alone vs alongside the flood.

### Milestone 6: stable, then CI
"Stable" means:
- `make bench` completes 3 times back-to-back with no manual steps and clean teardown each time.
- Run-to-run spread of median p99 per scenario is small enough to quote (the threshold is set after seeing real data).
- The report renders every section and chart without hand edits.

Only after that: add a GitHub Actions workflow and a README "Performance" section linking the latest headline report. Planned in detail when reached.

---

## 10. Later / not in this plan

- Check RPC latency in isolation (would need a gRPC load tool; vegeta is HTTP-only and there's no server-side histogram)
- Multi-instance Check over one Redis (over-admission across replicas)
- Token bucket comparison
- Profiling captures (pprof / flame graphs)
- Publishing reports (CI artifacts, gh-pages)
- Efficiency/footprint and engineering-quality metrics (excluded by decision)

---

## 11. CV / portfolio framing

Claims that read well have **a number, a condition, and a comparison or guarantee**. Illustrative templates (fill with real results):

- "Adds **< X ms p99** latency at **Y k RPS** vs an unprotected origin (single host, Docker, pinned CPUs)."
- "**0 over-admissions** across **N M** concurrent requests against real Redis under load."
- "Detects Redis failure and switches to fail-open/closed in **< X ms p95**, with **zero 5xx**."
- "Sustains **Y k RPS** at a p99 < 5 ms SLO; bottleneck identified as Check/Redis CPU."
- "Keeps well-behaved tenants' p99 within **X%** of baseline while one tenant floods at 10× its quota."

Portfolio artifacts, strongest first:
1. Latency-vs-throughput chart (baseline vs A vs B)
2. Failover timeline chart
3. Short methodology section (hardware, vegeta version, runs, warm-up, single-host disclosure)
