# README Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite all READMEs with clear layered separation (root = general architecture, modules = domain deep-dives) and slim CLAUDE.md to AI-only operational reference.

**Architecture:** 3-layer documentation: root README (architecture showcase), 8 module READMEs (domain + implementation notes), CLAUDE.md (build/run/gotchas). No duplication between layers.

**Tech Stack:** Markdown, Mermaid diagrams (kept from current auto-generated ER blocks)

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `README.md` | Rewrite | Architecture, design philosophy, cross-cutting conventions, infra overview |
| `internal/module/account/README.md` | Rewrite | Account domain: auth, profiles, contacts, favorites, payments, notifications |
| `internal/module/analytic/README.md` | Rewrite | Analytics domain: interactions, popularity scoring, recommendation feed |
| `internal/module/catalog/README.md` | Rewrite | Catalog domain: SPU/SKU, search, comments, recommendations |
| `internal/module/chat/README.md` | Rewrite | Chat domain: conversations, messaging, read receipts |
| `internal/module/common/README.md` | Rewrite | Common domain: resources, object storage, service options, geocoding, SSE |
| `internal/module/inventory/README.md` | Rewrite | Inventory domain: stock, serials, audit trail |
| `internal/module/order/README.md` | Rewrite | Order domain: cart, checkout, pending, payment, refunds |
| `internal/module/promotion/README.md` | Rewrite | Promotion domain: campaigns, price calculation, group stacking |
| `CLAUDE.md` | Slim down | Build/run, code gen, Restate gotchas, commit rules, frontend ref |

---

### Task 1: Rewrite Root README.md

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Write the new root README**

Replace the entire file with this content (wakatime badge preserved):

```markdown
# ShopNexus Server

[![wakatime](https://wakatime.com/badge/user/592c97c4-15ad-49cb-ac34-d607be35c524/project/79f8a24e-0fe8-417e-b42b-2009d7a4362f.svg)](https://wakatime.com/badge/user/592c97c4-15ad-49cb-ac34-d607be35c524/project/79f8a24e-0fe8-417e-b42b-2009d7a4362f)

A social marketplace backend in Go, built as a **modular monolith** designed for microservice extraction via [Restate](https://restate.dev) durable execution.

**No customer/vendor distinction** — any account can both buy and sell. Orders track `buyer_id` and `seller_id` per transaction.

> Development timeline: [timeline.md](timeline.md)

## Architecture

Entry point: `cmd/server/main.go` → `fx.New(app.Module).Run()`.

The server is a **modular monolith** — eight vertical-slice modules that each own their database schema, business logic, and HTTP transport. Modules communicate through Restate durable execution, meaning every cross-module call is an HTTP request to the Restate ingress. This gives us exactly-once delivery, automatic retries, and a clear extraction path to microservices — any module can be deployed as a standalone service by pointing its Restate registration to a different host.

Dependency injection is handled by [Uber fx](https://github.com/uber-go/fx). Each module's `fx.go` provides both the concrete `*XxxHandler` (registered with Restate) and the `XxxBiz` interface (a generated Restate proxy used by other modules and transport handlers).

### Module Structure

Every module under `internal/module/<name>/` follows the same vertical-slice layout:

```
biz/
  interface.go      # XxxBiz interface + XxxHandler struct + constructor + go:generate directive
  restate_gen.go    # Auto-generated Restate HTTP client (DO NOT EDIT)
  *.go              # Business logic methods (use restate.Context)
db/
  migrations/       # SQL schema (*.up.sql / *.down.sql)
  queries/          # SQLC query templates (pgtempl-generated + *_custom.sql for hand-written)
  sqlc/             # Generated DB code (DO NOT EDIT)
model/
  *.go              # DTOs, domain models, error sentinels
transport/echo/
  *.go              # HTTP handlers
fx.go               # Uber fx module wiring
```

## Restate Durable Execution

All business logic methods use `restate.Context` instead of `context.Context`. This is required for Restate's `Reflect()` registration and enables:

- **Durable side effects**: DB writes inside `restate.Run()` closures are journaled and replay-safe. If the process crashes mid-execution, Restate replays the journal and skips already-completed steps.
- **Cross-module RPC**: calls between modules go through auto-generated proxy clients (`XxxRestateClient`), which are HTTP calls to the Restate ingress. This makes every cross-module call durable and retryable.
- **Fire-and-forget**: `restate.ServiceSend(ctx, "ServiceName", "MethodName").Send(params)` for asynchronous work like notifications and analytics tracking — durable, exactly-once delivery.
- **Terminal errors**: client-facing errors (validation, not found, conflict) use `.Terminal()` to prevent Restate from retrying them.

### Integration Pattern

1. `interface.go` defines the `XxxBiz` interface, `XxxHandler` struct, `ServiceName()` method, and a `//go:generate` directive.
2. `restate_gen.go` is auto-generated by `cmd/genrestate/` — implements `XxxBiz` via HTTP calls to Restate ingress.
3. `fx.go` provides both `*XxxHandler` (for Restate registration) and `XxxBiz` (proxy, for cross-module deps and transport).
4. Transport handlers depend on `XxxBiz` (interface), never `*XxxHandler` (concrete).
5. `app/restate.go` registers all Handler structs with `restate.Reflect()` and auto-registers with the Restate admin API on startup.

## Database Design

Each module owns a PostgreSQL schema (`account.*`, `catalog.*`, `order.*`, etc.) — no cross-schema foreign keys. This enforces module boundaries at the database level and makes future extraction straightforward.

The database layer uses three tools:

- **[pgx/v5](https://github.com/jackc/pgx)** as the PostgreSQL driver, wrapped in `pgsqlc.Storage[T]` for connection pooling and transaction support.
- **[SQLC](https://sqlc.dev)** generates type-safe Go structs and query methods from SQL. Config in `sqlc.yaml`. Uses `guregu/null/v6` for nullable types.
- **pgtempl** (`cmd/pgtempl/`) generates SQLC query templates from migration files, producing CRUD queries automatically. Custom queries go in `*_custom.sql` files (not overwritten by pgtempl).

### Nullable Types

- `guregu/null/v6` for DB fields in SQLC-generated structs.
- `*T` pointers for JSON serialization in models exposed to Restate — generic types like `null.Value[T]` break Restate's JSON schema generation.
- `ptrutil.PtrIf[T](val, valid)` for converting nullable DB types to pointers.

## Code Conventions

### Naming

| Thing | Pattern | Example |
|-------|---------|---------|
| Interface (public API) | `XxxBiz` | `AccountBiz` |
| Implementation | `XxxHandler` | `AccountHandler` |
| Generated Restate proxy | `XxxRestateClient` | `AccountRestateClient` |
| Constructor | `NewXxxHandler` | `NewAccountHandler` |
| Import alias for shared model | `sharedmodel` | `internal/shared/model` |

### Error Handling

- **Client-visible errors**: `sharedmodel.NewError(httpStatusCode, "message")` defined in each module's `model/error.go`. Always return with `.Terminal()` to prevent Restate retries.
- **Internal errors**: `fmt.Errorf("action: %w", err)` — no "failed to" prefix (e.g., `fmt.Errorf("get account: %w", err)`).
- **Wrapping terminal errors**: use `sharedmodel.WrapErr(msg, err)` instead of `fmt.Errorf` — it preserves the terminal flag through the error chain.

### Code Generation Pipeline

Three generators run in sequence:

1. **pgtempl** → generates SQLC query templates from migration SQL
2. **SQLC** → generates type-safe Go from those query templates
3. **genrestate** → generates Restate proxy clients from `XxxBiz` interfaces

## Infrastructure

| Service | Package | Purpose |
|---------|---------|---------|
| PostgreSQL | `internal/shared/pgsqlc` | Multi-schema DB with pgxpool |
| Redis | `internal/infras/cache` | Struct caching (Sonic JSON serialization) |
| NATS | `internal/infras/pubsub` | Message queue (JetStream) |
| Restate | `internal/infras/restate` | Durable execution HTTP ingress |
| Milvus | `internal/infras/milvus` | Vector search for hybrid product search |
| S3/Local | `internal/infras/objectstore` | File storage with presigned URLs |

### External Providers

Payment and transport are pluggable via a `map[string]Client` pattern. Each provider implements a `Client` interface and is registered at startup.

| Provider | Package | Implementations |
|----------|---------|-----------------|
| Payment | `internal/provider/payment` | VNPay (QR/Bank/ATM), COD |
| Transport | `internal/provider/transport` | GHTK (Express/Standard/Economy) |
| Geocoding | `internal/provider/geocoding` | Nominatim (OpenStreetMap) |
| LLM | `internal/provider/llm` | OpenAI, AWS Bedrock, Python backend |

## Modules

Each module has its own README with ER diagrams, domain concepts, flows, and endpoints.

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
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "rewrite root README as architecture showcase"
```

---

### Task 2: Rewrite Account Module README

**Files:**
- Modify: `internal/module/account/README.md`

- [ ] **Step 1: Rewrite the file**

Keep the ER diagram block exactly as-is (auto-generated). Restructure the rest into: domain summary → ER diagram → domain concepts → implementation notes → endpoints → cross-module deps.

Key changes from current:
- Add a one-paragraph domain summary at the top
- Rename "Features" → "Domain Concepts" and tighten prose
- Add "Implementation Notes" section covering: JWT dual-secret rotation, bcrypt auth, partial unique index for default payment method, idempotent favorite add
- Keep endpoint tables as-is
- Add cross-module deps table (currently missing)

- [ ] **Step 2: Commit**

```bash
git add internal/module/account/README.md
git commit -m "rewrite account module README"
```

---

### Task 3: Rewrite Analytic Module README

**Files:**
- Modify: `internal/module/analytic/README.md`

- [ ] **Step 1: Rewrite the file**

Keep ER diagram. Restructure into template.

Key changes:
- Domain Concepts: explain interaction tracking and popularity scoring as separate concepts
- Flows: the 3-step fan-out flow (CreateInteraction → HandlePopularityEvent + Catalog.AddInteraction)
- Implementation Notes: fire-and-forget fan-out pattern, atomic upsert with no read-before-write, configurable weights
- Keep event weight table (valuable reference)
- Keep endpoints

- [ ] **Step 2: Commit**

```bash
git add internal/module/analytic/README.md
git commit -m "rewrite analytic module README"
```

---

### Task 4: Rewrite Catalog Module README

**Files:**
- Modify: `internal/module/catalog/README.md`

- [ ] **Step 1: Rewrite the file**

Keep ER diagram. Restructure into template.

Key changes:
- Domain Concepts: SPU/SKU model, polymorphic comments, tags, search_sync
- Flows: hybrid search flow (Milvus dense+sparse → fallback to PostgreSQL ILIKE), recommendation flow (Redis sorted sets → fallback to most-sold)
- Implementation Notes: background sync crons (metadata vs embedding), FOR UPDATE SKIP LOCKED for batch processing, pluggable LLM embedding provider, Milvus direct (no external search service)
- Keep endpoint tables
- Keep cross-module deps

- [ ] **Step 2: Commit**

```bash
git add internal/module/catalog/README.md
git commit -m "rewrite catalog module README"
```

---

### Task 5: Rewrite Chat Module README

**Files:**
- Modify: `internal/module/chat/README.md`

- [ ] **Step 1: Rewrite the file**

Keep ER diagram. Simple module — skip Flows section, minimal Implementation Notes.

Key changes:
- Domain Concepts: conversations (one per account pair, idempotent), messages (Text/Image/System types), read receipts
- Implementation Notes: just the legacy column name note (customer_id/vendor_id) and idempotent conversation creation
- Keep endpoints

- [ ] **Step 2: Commit**

```bash
git add internal/module/chat/README.md
git commit -m "rewrite chat module README"
```

---

### Task 6: Rewrite Common Module README

**Files:**
- Modify: `internal/module/common/README.md`

- [ ] **Step 1: Rewrite the file**

Keep ER diagram. Infrastructure-focused module — skip Flows, keep Implementation Notes focused on the polymorphic resource system and object storage backends.

Key changes:
- Domain Concepts: resource management (polymorphic refs), object storage (3 backends), service options registry, geocoding, SSE
- Implementation Notes: transactional replace-all for UpdateResources, auto-sync of service options on startup, placeholder image fallback
- Keep endpoints

- [ ] **Step 2: Commit**

```bash
git add internal/module/common/README.md
git commit -m "rewrite common module README"
```

---

### Task 7: Rewrite Inventory Module README

**Files:**
- Modify: `internal/module/inventory/README.md`

- [ ] **Step 1: Rewrite the file**

Keep ER diagram. Restructure into template.

Key changes:
- Domain Concepts: polymorphic stock refs, serial tracking, audit trail
- Flows: ImportStock → ReserveInventory → ReleaseInventory lifecycle
- Implementation Notes: SKIP LOCKED pattern for concurrent serial allocation, COPY FROM for bulk serial inserts, signed delta audit history
- Keep endpoints

- [ ] **Step 2: Commit**

```bash
git add internal/module/inventory/README.md
git commit -m "rewrite inventory module README"
```

---

### Task 8: Rewrite Order Module README

**Files:**
- Modify: `internal/module/order/README.md`

- [ ] **Step 1: Rewrite the file**

Keep ER diagram. Most complex module — all sections populated.

Key changes:
- Domain Concepts: pending items vs orders distinction, pluggable providers, two-phase flow
- Flows: full lifecycle (Cart → Checkout → Seller Confirms → Payment → Delivery → Refund) with numbered steps
- Implementation Notes: pluggable provider maps (paymentMap/transportMap), restate.Run() for transport/payment side effects, transport quote for cost preview, refund with PickUp/DropOff methods
- Keep endpoint tables (grouped by buyer/seller + pending/confirmed)
- Keep cross-module deps
- Keep provider info (VNPay, GHTK)

- [ ] **Step 2: Commit**

```bash
git add internal/module/order/README.md
git commit -m "rewrite order module README"
```

---

### Task 9: Rewrite Promotion Module README

**Files:**
- Modify: `internal/module/promotion/README.md`

- [ ] **Step 1: Rewrite the file**

Keep ER diagram. Restructure into template.

Key changes:
- Domain Concepts: unified promotion table with JSONB data, group-based stacking (different groups stack, same group competes, exclusive group wins all), polymorphic refs
- Flows: price calculation flow (collect codes + auto_apply → parse JSONB → filter by refs → group → pick winners → apply)
- Implementation Notes: JSONB for type-specific data (avoids table-per-type), schedule table exists but scheduler daemon not implemented
- Keep promotion types table
- Keep endpoints

- [ ] **Step 2: Commit**

```bash
git add internal/module/promotion/README.md
git commit -m "rewrite promotion module README"
```

---

### Task 10: Slim Down CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Rewrite CLAUDE.md**

Keep these sections as-is:
- Build & Run (make targets, docker compose, env vars)
- Code Generation (exact commands, pgtempl gotchas)
- Commit Rules
- Frontend

Slim these sections to a pointer:
- Architecture → remove entirely, add one line: "See [README.md](README.md) for architecture, conventions, and infrastructure. See each module's README for domain context."
- Naming Convention → remove (moved to root README)
- Conventions → remove (moved to root README)
- Key Infrastructure → remove (moved to root README)

Keep Restate gotchas but only the operational pitfalls:
- restate.Run() ctx is RunContext
- restate.Run() serialization issues
- Init-time proxy restriction
- genrestate 1-param rule
- Biz methods use restate.Context not context.Context
- Fire-and-forget syntax

Remove the full Restate Integration Pattern section (moved to root README).

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "slim down CLAUDE.md, point to READMEs for conventions"
```
