PROTO := proto/check/v1/check.proto
MODULE := github.com/dgallantino/api-rate-limiter

.PHONY: proto
proto:
	mkdir -p internal/gen
	protoc \
		--go_out=. --go_opt=module=$(MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
		$(PROTO)
