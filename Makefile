PROTO := proto/check/v1/check.proto
MODULE := github.com/dgallantino/api-rate-limiter
CONTAINER := $(shell command -v podman || command -v docker)
PY := $(shell test -x python/.venv/bin/python && echo $(abspath python/.venv/bin/python) || echo python3)
.PHONY: proto proto-go proto-python test test-race test-python run run-proxy run-origin run-origin-limited run-dashboard redis

proto: proto-go proto-python

proto-go:
	mkdir -p internal/gen
	protoc \
		--go_out=. --go_opt=module=$(MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
		$(PROTO)

proto-python:
	mkdir -p python/gen
	$(PY) -m grpc_tools.protoc \
		-I proto \
		--python_out=python/gen \
		--grpc_python_out=python/gen \
		$(PROTO)
	touch python/gen/check/__init__.py python/gen/check/v1/__init__.py

test: proto-go
	go test ./...

test-race: proto-go
	go test -race ./...

test-python: proto-python
	cd python && $(PY) -m pytest -q

run: proto-go
	go run ./cmd/check -config configs/check.example.yaml

run-proxy: proto
	go run ./cmd/proxy -config configs/proxy.example.yaml

run-origin:
	python3 -m uvicorn app:app --app-dir demo/origin --host 127.0.0.1 --port 8000

run-origin-limited:
	PYTHONPATH=python/src:python/gen python3 -m uvicorn limited:app --app-dir demo/origin --host 127.0.0.1 --port 8000

run-dashboard: proto-go
	go run ./cmd/dashboard -config configs/dashboard.example.yaml

# Real Redis for a manual cmd/check run. Tests use miniredis (no container).
redis:
	$(CONTAINER) run --rm -p 6379:6379 --name rl-redis redis:7-alpine
