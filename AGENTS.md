# Repository Guidelines

## Project Structure & Module Organization
- `cmd/<service>`: entrypoints for `api-gateway`, `user-service`, `wallet-service`, `watcher-service`; wire configs from `config/<service>.yaml`.
- `internal/<service>`: domain/service/server layers (e.g., wallet gRPC handlers, watcher scanners, user logic).
- `api/<service>/v1`: protobuf contracts plus generated Go files; edit `.proto`, regenerate stubs when schema changes.
- `pkg/`: shared utilities (`config` loader with Viper hot-reload, `logger`/zap, `trace` OTLP setup, `hdwallet`, `xredis`, `interceptor`, etc.).
- `deploy/`: docker-compose, k8s manifests, SQL seeds; `docker-compose.yaml` brings up MySQL, Redis, BTC/ETH nodes for local integration. `exec/` holds scratch examples.

## Build, Test, and Development Commands
- `go run ./cmd/api-gateway` (similarly `user-service`, `wallet-service`, `watcher-service`) to start a service; ensure matching config file exists.
- `go build ./cmd/api-gateway` to produce binaries per service.
- `go test ./...` for unit tests (uses `testify`); integration tests require deps from docker-compose.
- `docker-compose up -d` to start MySQL/Redis/BTC/ETH stacks used by wallet flows.
- `make build|test|run` exists but assumes `main.go` at repo root; prefer service-specific commands above.

## Coding Style & Naming Conventions
- Go 1.25; run `gofmt`/`goimports` before committing. Keep idiomatic Go naming: packages lower_snake, exported identifiers PascalCase, tests in `*_test.go`.
- Proto/gRPC contracts live under `api/*`; keep package options consistent when regenerating stubs.
- Prefer contextual logging via `pkg/logger`; keep service names consistent with etcd/trace registrations.

## Testing Guidelines
- Use table-driven tests with `testify/assert` where present; keep deterministic amounts via `decimal`.
- Name tests `TestXxx` colocated with implementation packages. Mock external systems where possible; otherwise document required docker-compose services.
- Run `go test ./...` before pushing; add integration checks for new DB or chain interactions.

## Commit & Pull Request Guidelines
- Git history favors short present-tense summaries (often Chinese), e.g., “日志收集”; keep one-line scope-first messages (`wallet: 修复充值流水`).
- Before PR: `gofmt`, `go test ./...`, update configs/migrations if defaults change.
- PRs should state affected service(s), config/env expectations, and test evidence; link issues/tickets and include logs/screenshots for API/grpc behavior when relevant.

## Security & Configuration Tips
- Configs load from `config/<service>.yaml`; env vars can override fields using uppercased service prefix (e.g., `GATEWAY_HTTP_ADDR` when service name `gateway`). Avoid committing secrets; use local env or vaults.
- Keep OTLP and etcd endpoints in environment or configs, not code. Clean up generated binaries in `bin/` before packaging artifacts.
