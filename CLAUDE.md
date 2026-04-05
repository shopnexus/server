# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

See [README.md](README.md) for architecture, conventions, and infrastructure. See each module's README under `internal/module/<name>/` for domain context.

## Build & Run

```bash
make dev          # hot-reload dev server (air)
make run          # direct server run
make build        # compile to bin/server
make generate     # run all go:generate (restate proxies, etc.)
make pgtempl      # generate SQLC query templates for all modules
make migrate      # run database migrations
make seed         # seed data
make register     # register services with Restate admin API
```

All `cmd/` packages use directory paths (`go run ./cmd/pgtempl/`) not file paths, since some have multiple files.

**Environment**: `GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn` is required (set in Makefile) due to proto file collision between restate-sdk and milvus.

**Infrastructure**: `docker compose -f deployment/docker-compose.yml up -d` starts PostgreSQL, Redis, NATS, Restate (3-node cluster), Milvus, MinIO.

## Code Generation

```bash
# Regenerate SQLC queries (always use -skip-schema-prefix)
go run ./cmd/pgtempl/ -module <name> -skip-schema-prefix -single-file=generated_queries.sql
# then: sqlc generate

# Regenerate Restate proxies after changing interface.go
go generate ./internal/module/<name>/biz/

# Regenerate all
make generate
```

The `genrestate` tool reads `interface.go`, finds the interface, and generates a Restate client struct. Methods must have exactly 1 input param (besides ctx). Multi-param methods must be wrapped in a params struct or made private.

pgtempl only handles `CREATE TABLE` and `ALTER TABLE ADD COLUMN` — it does NOT handle `DROP COLUMN`. After dropping columns, manually remove them from generated queries.

## Restate Gotchas

- **Biz methods use `restate.Context`** (not `context.Context`). This is required for `restate.Reflect()`. Exception: cron/init methods that use `context.Background()` call other methods through the Restate HTTP ingress client (`restateClient` field) instead of directly.
- **Fire-and-forget**: `restate.ServiceSend(ctx, "ServiceName", "MethodName").Send(params)` — durable, exactly-once.
- **Inside `restate.Run()` closures**, `ctx` is `restate.RunContext` which can't be passed to methods expecting `restate.Context` — move cross-module calls outside `Run()` blocks.
- **`restate.Run()` serialization**: return values are JSON-serialized in the journal. `uuid.UUID` (byte array), `null.*`, and `int64` may not round-trip. Avoid returning complex DB structs from `Run()`.
- **Init-time**: never call through the Restate proxy in constructors — Restate isn't running yet. Use background goroutines or `context.Context` helpers.

## Commit Rules

- **Short messages, no description body, no Co-Authored-By**.
- Imperative mood: `add`, `fix`, `refactor`, `rename`, `update`, `remove` — not `added`, `fixes`, `updating`.
- Examples: `add notifications feature`, `fix checkout item creation`, `rename structs to XxxHandler`.
- When recommitting (squash/rebase), group logically: one commit per feature/refactor, not per file.

## Frontend

Located at `../shopnexus-website-v3/`. Next.js 16 + React 19 + TanStack Query + shadcn/ui.

- API client: `lib/queryclient/custom-fetch.ts` — auto token refresh, redirect to `/login` on 401.
- Hooks: `core/<module>/*.ts` — one file per module with types + query/mutation hooks.
- Base URL: `https://shopnexus.hopto.org/api/v1/`.
- Dev: `bun dev` (port 5006).
