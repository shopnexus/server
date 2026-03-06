# System Module

## Overview

The **system** module implements the **Transactional Outbox Pattern** for reliable event publishing in the ShopNexus e-commerce backend. It provides the infrastructure to guarantee that domain events are eventually delivered to Kafka (via Watermill) even in the presence of process crashes or network failures.

The core idea: instead of publishing messages directly to Kafka during a business transaction (which risks message loss if the transaction succeeds but the publish fails, or vice versa), domain events are first written to an `outbox_event` table within the same database transaction as the business data change. A separate relay process then reads unprocessed events from the outbox and publishes them to Kafka, marking them as processed afterward.

The module follows the standard project layering: database migrations and SQLC-generated queries at the bottom, a business logic (`biz`) layer in the middle, and an Echo HTTP transport stub at the top, all wired together via Uber fx dependency injection.

---

## Database Schema

All tables live under the `system` PostgreSQL schema.

### Table: `system.outbox_event`

The outbox table that stores domain events awaiting delivery to the message broker.

```sql
CREATE SCHEMA IF NOT EXISTS "system";

CREATE TABLE IF NOT EXISTS "system"."outbox_event" (
    "id"             BIGSERIAL      NOT NULL,                          -- monotonically increasing event ID
    "topic"          VARCHAR(100)   NOT NULL,                          -- Kafka topic name
    "data"           JSONB          NOT NULL,                          -- serialized event payload
    "processed"      BOOLEAN        NOT NULL DEFAULT false,            -- whether the event has been relayed
    "date_processed" TIMESTAMPTZ(3),                                   -- when the event was relayed (NULL if pending)
    "date_created"   TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,-- when the event was inserted
    CONSTRAINT "outbox_event_pkey" PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "outbox_event_date_created_idx"
    ON "system"."outbox_event" ("date_created");
```

**Key design decisions:**

- **BIGSERIAL primary key** -- Provides a strictly ordered, gap-free sequence that the relay process can use for cursor-based polling (process events in insertion order).
- **JSONB data column** -- Stores the full event payload as structured JSON, allowing any event shape without schema changes.
- **processed flag + date_processed** -- The relay sets `processed = true` and records `date_processed` after successful publication to Kafka. This two-field approach allows both efficient filtering of pending events and audit/debugging of processed ones.
- **date_created index** -- Supports time-range queries for monitoring, cleanup, and debugging (e.g., "find all events created in the last hour that are still unprocessed").

### Migration Down

```sql
DROP INDEX IF EXISTS "outbox_event_date_created_idx";
DROP TABLE IF EXISTS "system"."outbox_event";
DROP SCHEMA IF EXISTS "system";
```

---
## API Endpoints

The system module registers a route group at `/api/v1/system` but currently exposes **no public HTTP endpoints**. The handler is a stub:

```go
func NewHandler(e *echo.Echo, biz *systembiz.SystemBiz) *Handler {
    h := &Handler{biz: biz}
    api := e.Group("/api/v1/system")
    _ = api
    return h
}
```

This is by design -- the outbox pattern operates internally. Events are produced by other modules writing to the outbox table within their own transactions, and consumed by the relay process publishing to Kafka. Administrative or monitoring endpoints can be added here in the future if needed.

---

## Business Logic Layer (`biz`)

### SystemBiz Struct

The central service object. Holds references to the database storage layer and a pub/sub client (Kafka/Watermill):

```go
type SystemBiz struct {
    storage SystemStorage
    pubsub  pubsub.Client
}
```

### Constructor

```go
func NewSystemBiz(
    storage SystemStorage,
    pubsub pubsub.Client,
) (*SystemBiz, error) {
    b := &SystemBiz{
        storage: storage,
        pubsub:  pubsub.Group("system"),
    }
    return b, errors.Join(
        b.SetupPubsub(),
    )
}
```

During initialization:
1. Wraps the injected pub/sub client with a `"system"` group prefix (topics will be prefixed accordingly).
2. Calls `SetupPubsub()` to register any subscribers or relay logic.

### SetupPubsub

Currently a placeholder that returns `nil`:

```go
func (b *SystemBiz) SetupPubsub() error {
    return nil
}
```

This is the designated hook where the outbox relay logic is implemented -- polling the `outbox_event` table for unprocessed events, publishing them via the `pubsub.Client`, and marking them as processed.

### Pub/Sub Client Interface

The `pubsub.Client` interface (from `internal/infras/pubsub`) provides:

```go
type Client interface {
    Group(name string) Client                                    // prefix topics with a group name
    Publish(topic string, value any) error                       // publish a message
    Subscribe(topic string, handler func(msg *MessageDecoder) error) error  // subscribe to a topic
    Close() error
}
```

This is backed by Watermill with Kafka as the message broker, providing at-least-once delivery semantics.

---

## Models and Types

### SystemOutboxEvent (SQLC-generated, in `db/sqlc/models.go`)

The Go representation of an outbox event row:

```go
type SystemOutboxEvent struct {
    ID            int64           `json:"id"`
    Topic         string          `json:"topic"`
    Data          json.RawMessage `json:"data"`
    Processed     bool            `json:"processed"`
    DateProcessed null.Time       `json:"date_processed"`
    DateCreated   time.Time       `json:"date_created"`
}
```

- `json.RawMessage` for `Data` allows the event payload to pass through without deserialization at the outbox layer.
- `null.Time` (from `github.com/guregu/null/v6`) for `DateProcessed` properly handles the nullable timestamp.

### SQLC-Generated Parameter Types

Key parameter structs used by the queries:

| Type | Used By | Purpose |
|------|---------|---------|
| `CreateOutboxEventParams` | `CreateOutboxEvent` | Full event creation with all fields specified. |
| `CreateDefaultOutboxEventParams` | `CreateDefaultOutboxEvent` | Minimal event creation (topic + data + optional date_processed). |
| `UpdateOutboxEventParams` | `UpdateOutboxEvent` | Partial update with COALESCE semantics and explicit NULL support. |
| `ListOutboxEventParams` | `ListOutboxEvent` | Flexible filtering with limit/offset pagination. |
| `ListCountOutboxEventParams` | `ListCountOutboxEvent` | Same as above plus total count via window function. |
| `CountOutboxEventParams` | `CountOutboxEvent` | Flexible filtering for count-only queries. |
| `DeleteOutboxEventParams` | `DeleteOutboxEvent` | Flexible filtering for bulk deletion (e.g., cleanup of old processed events). |

### Model Packages (Placeholder)

The `model/system.go` and `model/search.go` files exist as package stubs (empty beyond the `package systemmodel` declaration), reserved for future domain model definitions as the module grows.

---

## Key Patterns

### Transactional Outbox Pattern

The fundamental pattern this module implements. The flow is:

1. **Producer side** (other modules): Within a database transaction that modifies business data, the producer also inserts a row into `system.outbox_event` with the relevant topic and payload. Because both the business write and the event write are in the same transaction, they either both succeed or both fail -- guaranteeing consistency.

2. **Relay side** (this module): A background process periodically queries for unprocessed events (`processed = false`), publishes each one to Kafka, then marks it as `processed = true` with the current timestamp. If the relay crashes after publishing but before marking, the event will be re-published on the next poll (at-least-once delivery).

3. **Consumer side** (other modules/services): Subscribers consume events from Kafka topics. They must be idempotent to handle potential duplicate deliveries.

### Event Ordering Guarantees

Events are stored with a `BIGSERIAL` ID and the relay fetches them `ORDER BY "id"`. This ensures events are published in the exact order they were committed to the database, which is critical for maintaining causal ordering of domain events within a single aggregate.
### Pub/Sub Group Prefixing

The `pubsub.Group("system")` call wraps the pub/sub client so that all topics published or subscribed within this module are automatically prefixed. This provides namespace isolation between modules sharing the same Kafka cluster.

---
## How Other Modules Produce Events

Other modules in the system produce outbox events by obtaining a reference to the system module's storage (or directly using the database queries) and inserting into the outbox within their own transactions. A typical producer flow looks like:

```go
// Inside a transactional business operation in another module:
err := storage.WithTx(ctx, func(tx Storage) error {
    // 1. Perform the business data change
    _, err := tx.Querier().CreateOrder(ctx, orderParams)
    if err != nil {
        return err
    }

    // 2. Write the event to the outbox in the same transaction
    _, err = systemQuerier.CreateDefaultOutboxEvent(ctx, systemdb.CreateDefaultOutboxEventParams{
        Topic: "order.created",
        Data:  orderEventJSON,
    })
    return err
})
```

This guarantees that the event is recorded if and only if the business operation succeeds.

---