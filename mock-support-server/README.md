# Confidence Mock Support Server (Go)

Single-binary mock of Confidence HTTP and gRPC backends for provider benchmarks and tests.

## Features
- HTTP endpoints:
  - POST /v1/oauth/token → { accessToken, expiresIn }
  - GET /v1/resolverState:resolverStateUri → { signedUri, account }
  - GET /state.pb → serves resolver state bytes with ETag + 304
  - POST /v1/flags:resolve → ResolveFlagsResponse (JSON)
  - POST /v1/flagLogs:write → 200 OK (accepts application/x-protobuf)
  - GET /healthz → 200 OK
- gRPC endpoints:
  - confidence.flags.admin.v1.ResolverStateService (ResolverStateUri, FullResolverState)
  - confidence.flags.resolver.v1.InternalFlagLoggerService (WriteFlagLogs, WriteFlagAssigned)
- One process, one container. No sidecars needed.

Note: Public gRPC stubs for FlagResolverService are not generated here, so flags resolve is provided over HTTP only.

## Configuration (env)
- PORT_HTTP (default 8081)
- PORT_GRPC (default 9091)
- ACCOUNT_ID (default read from data/account_id)
- RESOLVER_STATE_PB (default data/resolver_state_current.pb if present)
- STATIC_TOKEN (default "mock-token")
- TOKEN_EXPIRES_IN (default 3600)
- FLAGS_RESPONSE_MODE (static|echo|file) [currently static]
- STATIC_FLAG_VARIANT (default flags/foo/variants/default)
- STATIC_FLAG_VALUE (default {"value":true})

## Build and run

Docker build:
```bash
docker build -f mock-support-server/Dockerfile -t cnfd-mock .
```

Run:
```bash
docker run --rm -p 8081:8081 -p 9091:9091 \
  -e ACCOUNT_ID=test-account \
  -e RESOLVER_STATE_PB=/data/resolver_state_current.pb \
  cnfd-mock
```

## Using with JS benchmark
- Override provider fetch to route Confidence hosts to http://mock:8081
- Or run the benchmark container in a compose network and target `mock:8081`

## Notes
- Extend as needed to support additional endpoints or behaviors.
- grpc-gateway can be added later if you want REST<->gRPC parity on one port.


