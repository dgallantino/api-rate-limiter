PROTO := proto/check/v1/check.proto
MODULE := github.com/dgallantino/api-rate-limiter

.PHONY: proto test test-race run redis

proto:
	mkdir -p internal/gen
	protoc \
		--go_out=. --go_opt=module=$(MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
		$(PROTO)

test: proto
	go test ./...

test-race: proto
	go test -race ./...

run: proto
	go run ./cmd/check -config configs/check.example.yaml

# Real Redis for a manual cmd/check run. Tests use miniredis (no container).
redis:
	podman run --rm -p 6379:6379 --name rl-redis redis:7-alpine
