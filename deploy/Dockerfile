FROM golang:1.26-alpine AS build
RUN apk add --no-cache git protobuf
ENV CGO_ENABLED=0
WORKDIR /src
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 \
	&& go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
COPY go.mod go.sum ./
RUN go mod download
COPY proto proto
COPY cmd cmd
COPY internal internal
COPY pkg pkg
RUN mkdir -p internal/gen && protoc \
	--go_out=. --go_opt=module=github.com/dgallantino/api-rate-limiter \
	--go-grpc_out=. --go-grpc_opt=module=github.com/dgallantino/api-rate-limiter \
	proto/check/v1/check.proto
RUN go build -o /out/check ./cmd/check \
	&& go build -o /out/proxy ./cmd/proxy \
	&& go build -o /out/dashboard ./cmd/dashboard \
	&& go build -o /out/origin ./cmd/origin

FROM golang:1.26-alpine AS vegeta-build
ARG VEGETA_VERSION=v12.13.0
RUN apk add --no-cache git
ENV CGO_ENABLED=0
RUN go install github.com/tsenart/vegeta/v12@${VEGETA_VERSION}

FROM alpine:3.21 AS runtime
RUN apk add --no-cache ca-certificates wget \
	&& mkdir -p /etc/api-rate-limiter

FROM runtime AS check
COPY --from=build /out/check /usr/local/bin/check
COPY configs/compose/check.yaml /etc/api-rate-limiter/check.yaml
EXPOSE 50051 2112
CMD ["check", "-config", "/etc/api-rate-limiter/check.yaml"]

FROM runtime AS proxy
COPY --from=build /out/proxy /usr/local/bin/proxy
COPY configs/compose/proxy.yaml /etc/api-rate-limiter/proxy.yaml
EXPOSE 8080
CMD ["proxy", "-config", "/etc/api-rate-limiter/proxy.yaml"]

FROM runtime AS dashboard
COPY --from=build /out/dashboard /usr/local/bin/dashboard
COPY configs/compose/dashboard.yaml /etc/api-rate-limiter/dashboard.yaml
EXPOSE 8081
CMD ["dashboard", "-config", "/etc/api-rate-limiter/dashboard.yaml"]

FROM runtime AS vegeta
COPY --from=vegeta-build /go/bin/vegeta /usr/local/bin/vegeta
CMD ["vegeta"]

FROM runtime AS origin
COPY --from=build /out/origin /usr/local/bin/origin
EXPOSE 8000
CMD ["origin"]

FROM runtime AS origin-limited
COPY --from=build /out/origin /usr/local/bin/origin
ENV CHECK_ADDR=check:50051
EXPOSE 8000
CMD ["origin", "-limited"]
