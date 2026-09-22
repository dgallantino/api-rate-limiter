PROTO := proto/check/v1/check.proto
MODULE := github.com/dgallantino/api-rate-limiter
CONTAINER := $(shell command -v podman || command -v docker)
REDIS_NAME := rl-redis
COMPOSE ?= $(shell docker info >/dev/null 2>&1 && echo docker compose || echo podman compose)
.PHONY: proto proto-go build test test-race run run-proxy run-origin run-origin-limited run-dashboard loadtest redis redis-stop redis-start compose-up compose-down compose-loadtest compose-redis-stop compose-redis-start

proto: proto-go

proto-go:
	mkdir -p internal/gen
	protoc \
		--go_out=. --go_opt=module=$(MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
		$(PROTO)

build: proto-go
	mkdir -p bin
	go build -o bin/check ./cmd/check
	go build -o bin/proxy ./cmd/proxy
	go build -o bin/dashboard ./cmd/dashboard
	go build -o bin/origin ./cmd/origin
	go build -o bin/loadtest ./cmd/loadtest

test: proto-go
	go test ./...

test-race: proto-go
	go test -race ./...

run: proto-go
	go run ./cmd/check -config configs/check.example.yaml

run-proxy: proto-go
	go run ./cmd/proxy -config configs/proxy.example.yaml

run-origin: proto-go
	go run ./cmd/origin

run-origin-limited: proto-go
	go run ./cmd/origin -limited

run-dashboard: proto-go
	go run ./cmd/dashboard -config configs/dashboard.example.yaml

loadtest:
	go run ./cmd/loadtest

# Real Redis for a manual cmd/check run. Tests use miniredis (no container).
redis:
	$(CONTAINER) start $(REDIS_NAME) 2>/dev/null || $(CONTAINER) run -d -p 6379:6379 --name $(REDIS_NAME) redis:7-alpine

redis-stop:
	$(CONTAINER) stop $(REDIS_NAME)

redis-start:
	$(CONTAINER) start $(REDIS_NAME)

compose-up:
	$(COMPOSE) up --build

compose-down:
	$(COMPOSE) down --remove-orphans

compose-loadtest:
	$(COMPOSE) --profile load run --rm loadtest

compose-redis-stop:
	$(COMPOSE) stop redis

compose-redis-start:
	$(COMPOSE) start redis
