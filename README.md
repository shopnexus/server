# ShopNexus Server

[![wakatime](https://wakatime.com/badge/github/shopnexus/shopnexus-server.svg)](https://wakatime.com/badge/github/shopnexus/shopnexus-server)

A marketplace backend in Go — **microservices in a monorepo**, orchestrated by [Restate](https://restate.dev) durable execution.

> Development timeline: [timeline.md](assets/timeline.md)
>
> Code convention: [convention.md](assets/convention.md)

## Architecture

Eight vertical-slice modules, each owning their database schema, business logic, and HTTP transport. Modules communicate through Restate durable execution — every cross-module call is an HTTP request to the Restate ingress, giving exactly-once delivery and automatic retries. Each module can be deployed as a standalone service by pointing its Restate registration to a different host.

## Request Flow

Every call goes through a **proxy interface** that mirrors each service's method signatures — callers invoke it as if it were the service itself, while the proxy forwards the request over HTTP to the Restate ingress, which then routes it to the target service.

![flow1.jpg](assets/flow1.png)

Cross-service calls take the exact same path — Service A never calls Service B directly. Both external traffic and inter-service calls fan in through the proxy and the Restate ingress, so durability, retries, and observability apply uniformly to every call in the system.

![flow2.jpg](assets/flow2.png)

For example, the order module depends on `InventoryBiz` as an **interface**, so the call site reads like an ordinary in-process method call:

```go
// Module "order" calling to "inventory" through the proxy interface
inventories, err := orderbiz.inventory.ReserveInventory(ctx, inventorybiz.ReserveInventoryParams{
    OrderID: order.ID,
    Items:   items,
})
```

## Restate Durable Execution

All business logic methods use `restate.Context` instead of `context.Context`. This is required for Restate's `Reflect()` registration and enables:

- **Durable side effects**: DB writes inside `restate.Run()` closures are journaled and replay-safe. If the process crashes mid-execution, Restate replays the journal and skips already-completed steps.
- **Cross-module RPC**: calls between modules go through auto-generated proxy clients (`XxxRestateClient`), which are HTTP calls to the Restate ingress. This makes every cross-module call durable and retryable.
- **Fire-and-forget**: `restate.ServiceSend(ctx, "ServiceName", "MethodName").Send(params)` for asynchronous work like notifications and analytics tracking — durable, exactly-once delivery.
- **Terminal errors**: client-facing errors (validation, not found, conflict) use `.Terminal()` to prevent Restate from retrying them.

## Tooling

- **[pgx/v5](https://github.com/jackc/pgx)** as the PostgreSQL driver, wrapped in `pgsqlc.Storage[T]` for connection pooling and transaction support.
- **[SQLC](https://sqlc.dev)** generates type-safe Go structs and query methods from SQL. Config in `sqlc.yaml`. Uses `guregu/null/v6` for nullable types.
- **pgtempl** (`cmd/pgtempl/`) generates SQLC query templates from migration files, producing CRUD queries automatically. Custom queries go in `*_custom.sql` files (not overwritten by pgtempl).

## Distributed Locking

```go
unlock := b.locker.Lock(ctx, "order:payment:123")
defer unlock()
```

- **Auto-renewal**: a background goroutine extends the TTL every `ttl/2`, so long-running handlers never lose the lock. The `unlock()` func stops the goroutine and DELs the key.
- **TTL** is configured once via `locker.Config{TTL: 30 * time.Second}` at construction time, not per call.

### Choosing a lock key

Lock by the **entity that owns the mutation**, not the entity being mutated. Three questions:

1. **Who causes the mutation?** Lock by the actor's scope — `sellerID`, `paymentID`, `refundID`. Not by individual rows being modified.
2. **Batch or single?** If the operation takes a batch of entities (e.g., seller confirms multiple items), the lock scope must contain all of them. Locking per-item in a batch risks deadlock when two requests lock items in different order.
3. **Would any request need multiple locks?** If yes, escalate to a coarser scope (e.g., items → seller) to eliminate circular-wait deadlocks. Only use fine-grained locks when coarse locking is a measured bottleneck.

```go
func handler() {
  unlock := b.locker.Lock(ctx, fmt.Sprintf("order:seller-pending:%s", params.Account.ID))
  defer unlock()
  // Logic
}
```

## Modules

Each module has its own README with ER diagrams, domain concepts, flows, and endpoints.

| Module                                       | Description                                                            |
| -------------------------------------------- | ---------------------------------------------------------------------- |
| [`account`](internal/module/account/)        | Auth, profiles, contacts, favorites, payment methods, notifications    |
| [`catalog`](internal/module/catalog/)        | Products (SPU/SKU), categories, tags, comments, hybrid search          |
| [`order`](internal/module/order/)            | Cart, checkout, pending items, seller confirmation, payment, refunds   |
| [`inventory`](internal/module/inventory/)    | Stock management, serial tracking, audit history                       |
| [`promotion`](internal/module/promotion/)    | Discounts, ship discounts, scheduling, group-based price stacking      |
| [`analytic`](internal/module/analytic/)      | Interaction tracking, weighted product popularity scoring              |
| [`chat`](internal/module/chat/)              | REST messaging, conversations, read receipts                           |
| [`common`](internal/module/common/)          | Resource/file management, object storage, service options, SSE         |
