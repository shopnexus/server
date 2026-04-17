# README Rewrite Design

Rewrite all READMEs in the backend project with clear separation of concerns between root (general) and module (domain-specific) documentation. Slim down CLAUDE.md to remove duplication.

## Goals

- Root README: architecture showcase for onboarding + portfolio
- Module READMEs: domain deep-dives with implementation notes
- CLAUDE.md: AI-only operational reference, points to READMEs for conventions
- No duplication across the three layers

## Audience

Both contributors onboarding to the codebase and external readers (portfolio, thesis, interviews).

## Files Affected

- `README.md` (root) — rewrite
- `internal/module/account/README.md` — rewrite
- `internal/module/analytic/README.md` — rewrite
- `internal/module/catalog/README.md` — rewrite
- `internal/module/chat/README.md` — rewrite
- `internal/module/common/README.md` — rewrite
- `internal/module/inventory/README.md` — rewrite
- `internal/module/order/README.md` — rewrite
- `internal/module/promotion/README.md` — rewrite
- `CLAUDE.md` — slim down, remove sections moved to README

## Root README Structure

```
# ShopNexus Server
  - One-paragraph project description (social marketplace, modular monolith, Restate)
  - "No customer/vendor distinction" callout

## Architecture
  - Entry point (fx.New → app.Module)
  - Modular monolith philosophy — why vertical slices, why Restate
  - Module structure diagram (biz/, db/, model/, transport/)
  - Module table with links

## Restate Durable Execution
  - Why Restate (durability, replay-safety, microservice extraction)
  - Patterns: restate.Context, restate.Run(), ServiceSend
  - Cross-module proxy clients
  - Terminal errors

## Database Design
  - One schema per module
  - SQLC + pgtempl pipeline
  - Nullable types (guregu/null vs *T pointers)

## Code Conventions
  - Naming (XxxBiz, XxxHandler, XxxRestateClient, import aliases)
  - Error handling (sharedmodel.NewError, WrapErr, no "failed to")
  - Code generation pipeline (pgtempl → sqlc → genrestate)

## Infrastructure
  - Service table (PostgreSQL, Redis, NATS, Milvus, S3, Geocoding, LLM)
  - Provider pattern (payment, transport — pluggable via map[string]Client)

## Modules
  - Table linking to each module README
```

No setup/quick start section — that stays in CLAUDE.md.

## Module README Template

Each module follows this template, with sections scaled to complexity:

```
# <Module> Module
  - One-paragraph domain summary
  - Handler/Interface/Service name line

## ER Diagram
  - Mermaid block at the top (auto-generated, kept from current)

## Domain Concepts
  - Key entities and relationships in prose
  - Domain-specific rules

## Flows
  - Business flows as numbered steps or text diagrams
  - Only for modules with multi-step flows (order, catalog, analytic)
  - Omitted for simple modules (chat, common)

## Implementation Notes
  - Non-obvious patterns developers need to know
  - Scaled per module: rich for order/catalog/analytic, minimal or omitted for chat/common

## Endpoints
  - Grouped endpoint tables (same format as current)

## Cross-Module Dependencies
  - Table: module → usage
```

## CLAUDE.md Slim-Down

Keep:
- Build & Run (make targets, docker compose, env var notes)
- Code Generation (exact commands, pgtempl gotchas)
- Restate Gotchas (RunContext vs restate.Context, serialization, init-time, genrestate rules)
- Commit Rules
- Frontend section
- Pointer: "See README.md for architecture and conventions. See module READMEs for domain context."

Remove (moved to root README):
- Architecture section
- Module Structure diagram
- Naming table
- Error Handling section
- Nullable Types section
- Infrastructure table
- Key Infrastructure table

## Design Decisions

- **ER diagrams at the top**: grounds the reader in the data model before explaining flows
- **No duplication**: conventions live in root README only, CLAUDE.md references them
- **Implementation Notes section**: covers non-obvious patterns (B option from brainstorming) without going to function-level docs
- **Template scales to complexity**: simple modules (chat, common) skip Flows and have minimal Implementation Notes
