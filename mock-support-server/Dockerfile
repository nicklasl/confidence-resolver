FROM golang:1.24-bookworm AS build
WORKDIR /src/mock-support-server

# Install protoc and Go plugins
RUN apt-get update && apt-get install -y --no-install-recommends \
      protobuf-compiler libprotobuf-dev \
    && rm -rf /var/lib/apt/lists/*
ENV PATH="/go/bin:${PATH}"
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

# Module files and sources
COPY mock-support-server/go.mod ./go.mod
COPY mock-support-server/main.go ./main.go

COPY mock-support-server/proto /src/mock-support-server/proto

# Generate minimal Go code for IAM auth service into local genproto
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p genproto && \
    protoc -I /src/mock-support-server/proto \
      --go_out=paths=source_relative:genproto \
      --go-grpc_out=paths=source_relative:genproto \
      mock/iam/v1/iam.proto && \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mock-support-server .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/mock-support-server /mock-support-server
COPY data/account_id /data/account_id
COPY data/resolver_state_current.pb /data/resolver_state_current.pb
ENV PORT_HTTP=8081 PORT_GRPC=9091
EXPOSE 8081 9091
ENTRYPOINT ["/mock-support-server"]


