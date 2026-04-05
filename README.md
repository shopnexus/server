# ShopNexus Server

[![wakatime](https://wakatime.com/badge/user/592c97c4-15ad-49cb-ac34-d607be35c524/project/79f8a24e-0fe8-417e-b42b-2009d7a4362f.svg)](https://wakatime.com/badge/user/592c97c4-15ad-49cb-ac34-d607be35c524/project/79f8a24e-0fe8-417e-b42b-2009d7a4362f)

A social marketplace backend in Go, built as a **modular monolith** designed for microservice extraction via [Restate](https://restate.dev) durable execution.

**No customer/vendor distinction** — any account can both buy and sell. Orders track `buyer_id` and `seller_id` per transaction.

> Development timeline: [timeline.md](timeline.md)

## Modules

Each module has its own README with ER diagrams, endpoints, and domain concepts.

| Module | Description |
|--------|-------------|
| [`account`](internal/module/account/) | Auth, profiles, contacts, favorites, payment methods, notifications |
| [`catalog`](internal/module/catalog/) | Products (SPU/SKU), categories, tags, comments, hybrid search, recommendations |
| [`order`](internal/module/order/) | Cart, checkout, pending items, seller confirmation, payment, refunds |
| [`inventory`](internal/module/inventory/) | Stock management, serial tracking, audit history |
| [`promotion`](internal/module/promotion/) | Discounts, ship discounts, scheduling, group-based price stacking |
| [`analytic`](internal/module/analytic/) | Interaction tracking, weighted product popularity scoring |
| [`chat`](internal/module/chat/) | REST messaging, conversations, read receipts |
| [`common`](internal/module/common/) | Resource/file management, object storage, service options, SSE, geocoding |

## Quick Start

```bash
# Infrastructure
docker compose -f deployment/docker-compose.yml up -d

# Server
make dev          # hot-reload (air)
make migrate      # run migrations
make seed         # seed data
make register     # register with Restate
```

## Architecture

Entry point: `cmd/server/main.go` → `fx.New(app.Module).Run()`

### Module Structure (Vertical Slice)

```
internal/module/<name>/
  biz/
    interface.go      # XxxBiz interface + XxxHandler struct + constructor
    restate_gen.go    # Auto-generated Restate proxy (DO NOT EDIT)
    *.go              # Business logic (uses restate.Context)
  db/
    migrations/       # SQL schema (*.up.sql / *.down.sql)
    queries/          # SQLC queries (pgtempl-generated + *_custom.sql)
    sqlc/             # Generated DB code (DO NOT EDIT)
  model/              # DTOs, domain models, error sentinels
  transport/echo/     # HTTP handlers
  fx.go               # Uber fx module wiring
```

### Restate Durable Execution

All biz methods use `restate.Context` instead of `context.Context`:

- **Durable side effects**: DB writes inside `restate.Run()` are journaled and replay-safe
- **Cross-module RPC**: calls go through generated proxy clients (`XxxRestateClient`)
- **Fire-and-forget**: `restate.ServiceSend()` for notifications, analytics — durable, exactly-once
- **Terminal errors**: client-facing errors use `.Terminal()` to prevent retries

Cross-module deps use the `XxxBiz` interface (resolved to Restate proxy by fx). Transport handlers also depend on the interface, never the concrete struct.

### Database

- **PostgreSQL**: one schema per module (`account.*`, `catalog.*`, `order.*`, etc.)
- **SQLC**: type-safe Go from SQL queries. `guregu/null/v6` for nullable types, `pgx/v5` driver.
- **pgtempl**: generates SQLC query templates from migrations. Custom queries in `*_custom.sql`.

### Infrastructure

| Service | Package | Purpose |
|---------|---------|---------|
| PostgreSQL | `internal/shared/pgsqlc` | Multi-schema DB with pgxpool |
| Redis | `internal/infras/cachestruct` | Struct caching (Sonic JSON) |
| NATS | `internal/infras/pubsub` | Message queue (JetStream) |
| Restate | `internal/infras/restate` | HTTP ingress client for service calls |
| Milvus | `internal/infras/milvus` | Vector search |
| S3/Local | `internal/infras/objectstore` | File storage with presigned URLs |
| Geocoding | `internal/infras/geocoding` | Reverse/forward geocoding + search |
| LLM | `internal/infras/llm` | Embedding + chat (Python/OpenAI/Bedrock) |

## Code Rules

### Error Handling

- **Client-visible errors**: `sharedmodel.NewError(httpStatusCode, "message")` defined in `model/error.go`. Return with `.Terminal()` to prevent Restate retries.
- **Internal errors**: `fmt.Errorf("action: %w", err)` — no "failed to" prefix.
- **Wrapping terminal errors**: use `sharedmodel.WrapErr(msg, err)` instead of `fmt.Errorf` to preserve the terminal flag.

### Naming

| Thing | Pattern | Example |
|-------|---------|---------|
| Interface | `XxxBiz` | `AccountBiz` |
| Implementation | `XxxHandler` | `AccountHandler` |
| Generated proxy | `XxxRestateClient` | `AccountRestateClient` |
| Constructor | `NewXxxHandler` | `NewAccountHandler` |
| Import alias | `sharedmodel` | `internal/shared/model` |

### Nullable Types

- `guregu/null/v6` for DB fields in SQLC-generated structs
- `*T` pointers for JSON serialization in models exposed to Restate (generic types like `null.Value[T]` break Restate's JSON schema generation)
- `ptrutil.PtrIf[T](val, valid)` for converting nullable DB types to pointers

### Restate Gotchas

- **`restate.Run()` closures**: `ctx` inside is `restate.RunContext`, not `restate.Context` — move cross-module calls outside `Run()` blocks.
- **`restate.Run()` serialization**: return values are JSON-serialized in the journal. `uuid.UUID` (byte array), `null.*`, `int64` may not round-trip. Avoid returning complex DB structs.
- **Init-time**: never call through the Restate proxy in constructors (Restate isn't running yet). Use background goroutines or `context.Context` helpers.
- **`genrestate`**: methods must have exactly 1 input param (besides ctx). Multi-param methods need a params struct.

### Code Generation

```bash
go run ./cmd/pgtempl/ -module <name> -skip-schema-prefix -single-file=generated_queries.sql
sqlc generate
go generate ./internal/module/<name>/biz/
# or: make generate (all modules)
```
