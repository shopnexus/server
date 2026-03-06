# Inventory Module

## Overview

The Inventory module is responsible for all stock management within the ShopNexus e-commerce platform. It provides a complete solution for tracking product stock levels, managing individual serial numbers for serialized products, recording audit trails of every stock change, and reserving inventory during order placement. The module uses a polymorphic reference design (`ref_type` + `ref_id`) so that a single stock/serial system can serve both Product SKUs and Promotions.

**Tech stack:** Go, PostgreSQL (pgx v5), SQLC v1.30, Echo v4, Uber fx

---
## Database Schema

All tables live under the dedicated PostgreSQL schema `inventory`.

### Enums

```sql
CREATE TYPE "inventory"."stock_ref_type" AS ENUM ('ProductSku', 'Promotion');
CREATE TYPE "inventory"."status" AS ENUM ('Active', 'Inactive', 'Taken', 'Damaged');
```

| Enum | Values | Purpose |
|------|--------|---------|
| `stock_ref_type` | `ProductSku`, `Promotion` | Discriminator for what entity a stock or serial row belongs to |
| `status` | `Active`, `Inactive`, `Taken`, `Damaged` | Lifecycle state of a serial number |

### Tables

#### `inventory.serial`

Tracks individual serial numbers. Each serial is tied to a reference entity via the polymorphic `(ref_type, ref_id)` pair.

```sql
CREATE TABLE IF NOT EXISTS "inventory"."serial" (
    "id"           TEXT         NOT NULL,
    "ref_type"     "inventory"."stock_ref_type" NOT NULL,
    "ref_id"       UUID         NOT NULL,
    "status"       "inventory"."status" NOT NULL DEFAULT 'Active',
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "serial_pkey" PRIMARY KEY ("id")
);
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | `TEXT` | Primary key. Either vendor-provided or auto-generated UUID string. |
| `ref_type` | `stock_ref_type` | The type of entity this serial belongs to (`ProductSku` or `Promotion`). |
| `ref_id` | `UUID` | The UUID of the referenced entity (e.g., the product SKU ID). |
| `status` | `status` | Current lifecycle state. Defaults to `Active`. |
| `date_created` | `TIMESTAMPTZ(3)` | Timestamp of creation with millisecond precision. |

#### `inventory.stock`

Aggregated stock counters for a given reference entity. Each `(ref_type, ref_id)` pair has exactly one stock row (enforced by a unique index).

```sql
CREATE TABLE IF NOT EXISTS "inventory"."stock" (
    "id"              BIGSERIAL NOT NULL,
    "ref_type"        "inventory"."stock_ref_type" NOT NULL,
    "ref_id"          UUID      NOT NULL,
    "stock"           BIGINT    NOT NULL,
    "taken"           BIGINT    NOT NULL DEFAULT 0,
    "serial_required" BOOLEAN   NOT NULL DEFAULT false,
    "date_created"    TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "stock_pkey" PRIMARY KEY ("id")
);
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | `BIGSERIAL` | Auto-increment primary key. |
| `ref_type` | `stock_ref_type` | Polymorphic discriminator. |
| `ref_id` | `UUID` | The UUID of the referenced entity. |
| `stock` | `BIGINT` | Current available stock count. |
| `taken` | `BIGINT` | Cumulative count of items reserved/sold. Defaults to `0`. |
| `serial_required` | `BOOLEAN` | Whether serial number tracking is required for this stock. Defaults to `false`. |
| `date_created` | `TIMESTAMPTZ(3)` | Row creation timestamp. |

#### `inventory.stock_history`

Append-only audit log recording every stock change (imports, adjustments).

```sql
CREATE TABLE IF NOT EXISTS "inventory"."stock_history" (
    "id"           BIGSERIAL NOT NULL,
    "stock_id"     BIGINT    NOT NULL,
    "change"       BIGINT    NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "stock_history_pkey" PRIMARY KEY ("id")
);
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | `BIGSERIAL` | Auto-increment primary key. |
| `stock_id` | `BIGINT` | Foreign key to `inventory.stock(id)`. Cascades on delete and update. |
| `change` | `BIGINT` | The delta applied. Positive for imports, negative for deductions. |
| `date_created` | `TIMESTAMPTZ(3)` | Timestamp of the change. |

### Indexes

```sql
CREATE INDEX  "serial_ref_id_ref_type_idx"    ON "inventory"."serial"        ("ref_id", "ref_type");
CREATE UNIQUE INDEX "stock_ref_id_ref_type_key" ON "inventory"."stock"       ("ref_id", "ref_type");
CREATE INDEX  "stock_history_stock_id_idx"     ON "inventory"."stock_history" ("stock_id");
CREATE INDEX  "stock_history_date_created_idx" ON "inventory"."stock_history" ("date_created");
```

| Index | Table | Columns | Unique | Purpose |
|-------|-------|---------|--------|---------|
| `serial_ref_id_ref_type_idx` | `serial` | `(ref_id, ref_type)` | No | Fast lookup of serials by reference entity |
| `stock_ref_id_ref_type_key` | `stock` | `(ref_id, ref_type)` | **Yes** | Ensures one stock row per entity; used by `GetStock` |
| `stock_history_stock_id_idx` | `stock_history` | `(stock_id)` | No | Fast join/filter on the parent stock row |
| `stock_history_date_created_idx` | `stock_history` | `(date_created)` | No | Time-range queries on audit history |

### Foreign Keys

```sql
ALTER TABLE "inventory"."stock_history"
    ADD CONSTRAINT "stock_history_stock_id_fkey"
    FOREIGN KEY ("stock_id") REFERENCES "inventory"."stock" ("id")
    ON DELETE CASCADE ON UPDATE CASCADE;
```

Deleting a stock row will cascade-delete all its history entries.

---

## API Endpoints

All routes are registered under the `/api/v1/inventory` prefix via Echo v4.

### Stock Endpoints

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/api/v1/inventory/stock` | `GetStock` | Retrieve a single stock record by `ref_id` and `ref_type` |
| `GET` | `/api/v1/inventory/stock/history` | `ListStockHistory` | Paginated list of stock change history for a given entity |
| `POST` | `/api/v1/inventory/stock/import` | `ImportStock` | Import/add stock for an entity; creates a history record and publishes an event |

### Serial Endpoints

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/api/v1/inventory/serial` | `ListSerial` | Paginated list of serial numbers for a given entity |
| `PATCH` | `/api/v1/inventory/serial` | `UpdateSerial` | Batch-update the status of one or more serials |

### Request / Response Details

#### GET `/api/v1/inventory/stock`

Query parameters:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `ref_id` | `UUID` | Yes | The referenced entity UUID |
| `ref_type` | `string` | Yes | `ProductSku` or `Promotion` |

Returns a single `Stock` object.

#### GET `/api/v1/inventory/stock/history`

Query parameters:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `ref_id` | `UUID` | Yes | The referenced entity UUID |
| `ref_type` | `string` | Yes | `ProductSku` or `Promotion` |
| `page` | `int32` | No | Page number (1-based) |
| `limit` | `int32` | No | Items per page (max 100) |

Returns a paginated result of `StockHistory` objects.

#### POST `/api/v1/inventory/stock/import`

JSON body:

```json
{
  "ref_id": "uuid-string",
  "ref_type": "ProductSku",
  "change": 100,
  "serial_ids": ["SN001", "SN002"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ref_id` | `UUID` | Yes | The referenced entity UUID |
| `ref_type` | `string` | Yes | `ProductSku` or `Promotion` |
| `change` | `int64` | Yes | Number of items to add (must be > 0) |
| `serial_ids` | `[]string` | No | Optional vendor-provided serial numbers |

#### GET `/api/v1/inventory/serial`

Query parameters:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `ref_id` | `UUID` | Yes | The referenced entity UUID |
| `ref_type` | `string` | Yes | `ProductSku` or `Promotion` |
| `page` | `int32` | No | Page number |
| `limit` | `int32` | No | Items per page (max 100) |

Returns a paginated result of `Serial` objects.

#### PATCH `/api/v1/inventory/serial`

JSON body:

```json
{
  "serial_ids": ["SN001", "SN002"],
  "status": "Damaged"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `serial_ids` | `[]string` | Yes | List of serial IDs to update |
| `status` | `string` | Yes | New status: `Active`, `Inactive`, `Taken`, or `Damaged` |

---

## Stock Management Logic

### Creating Stock

`CreateStock` initializes a new stock record for a `(ref_type, ref_id)` pair within a database transaction. It calls `CreateDefaultStock` which inserts a row with the initial `stock` count, `taken` defaulting to `0`, and `serial_required` defaulting to `false`.

### Importing Stock

`ImportStock` handles stock replenishment:

1. Looks up the existing stock row by `(ref_type, ref_id)`.
2. Creates a `stock_history` entry recording the positive `change` value.
3. Publishes an `inventory.stock.updated` event to the internal pub/sub bus.
4. The event subscriber (`InventoryStockUpdated`) then:
   - If the `ref_type` is `ProductSku`, creates serial number records (either from vendor-supplied IDs or auto-generated UUIDs) using PostgreSQL `COPY FROM` for high-throughput bulk inserts.
   - Increments the `stock` column on the stock row by the `change` amount via `UpdateCurrentStock`.

All of steps 4a and 4b happen within a single database transaction.

### Reserving Inventory (Order Placement)

`ReserveInventory` is the core transactional method used when placing orders. It accepts multiple items and processes them all within a single database transaction:

For each item in the reservation:

1. **Fetch stock** -- retrieves the stock row by `(ref_type, ref_id)`.
2. **Availability check** -- if `stock < amount`, returns `ErrOutOfStock`.
3. **Adjust counters** -- atomically decrements `stock` and increments `taken` via `AdjustInventory`. The SQL includes a guard clause (`WHERE stock >= amount`) to prevent negative stock at the database level.
4. **Serial allocation** (if `serial_required` is `true`):
   - Fetches available serials using `FOR UPDATE SKIP LOCKED` (see [Serial Number Tracking](#serial-number-tracking) below).
   - Verifies the correct number of serials were obtained; returns `ErrOutOfStock` if not.
   - Marks the allocated serials as `Taken`.
5. **Returns results** -- returns the list of allocated serial IDs per item.

### Listing Most Taken SKUs

`ListMostTakenSku` returns stock records ordered by `taken DESC` -- useful for analytics dashboards showing best-selling products.

---

## Serial Number Tracking

### The SKIP LOCKED Pattern

The serial reservation query is the most concurrency-critical part of the module:

```sql
SELECT id, ref_type, ref_id
FROM "inventory"."serial"
WHERE ref_type = $1 AND ref_id = $2 AND "status" = 'Active'
ORDER BY date_created DESC
FOR UPDATE SKIP LOCKED
LIMIT $3;
```

**How it works:**

- `FOR UPDATE` -- acquires a row-level exclusive lock on the selected rows, preventing other transactions from modifying them.
- `SKIP LOCKED` -- instead of blocking when a row is already locked by another transaction, the query silently skips those rows and picks the next available ones.

**Why this matters for e-commerce:**

This pattern is essential for high-concurrency inventory systems. When multiple customers attempt to purchase the same product simultaneously:

1. Transaction A locks serials 1, 2, 3.
2. Transaction B runs the same query but SKIP LOCKED causes it to receive serials 4, 5, 6 instead of blocking.
3. Neither transaction waits on the other, eliminating contention-related deadlocks and timeouts.
4. If Transaction A rolls back, serials 1, 2, 3 become available again for future queries.

### Serial Lifecycle

```
Active  -->  Taken     (reserved during order placement)
Active  -->  Inactive  (manually deactivated)
Active  -->  Damaged   (marked as damaged)
Taken   -->  Active    (returned/refunded, if applicable)
```

### Serial ID Generation

When importing stock for `ProductSku` entities, serials are created in one of two ways:

- **Vendor-provided**: if the `serial_ids` array is supplied and its length matches `change`, those exact IDs are used.
- **Auto-generated**: if `serial_ids` is empty, UUIDs are generated via `uuid.NewString()`.

Bulk serial creation uses PostgreSQL's `COPY FROM` protocol (`CopyFrom` in pgx) for maximum throughput, avoiding the overhead of individual INSERT statements.

---

## Audit History

Every stock import creates a `stock_history` record containing:

- `stock_id` -- which stock row was affected.
- `change` -- the signed delta (positive for imports).
- `date_created` -- when the change occurred.

The `ListStockHistory` business method retrieves paginated history for a specific entity by:

1. Looking up the `stock` row by `(ref_type, ref_id)`.
2. Counting total history records for pagination metadata.
3. Returning the requested page of `StockHistory` records.

The SQLC queries for stock history support flexible filtering including range filters on `change` and `date_created`, making it suitable for time-windowed reporting.

---

## Pub/Sub Events

The module uses an internal pub/sub bus for decoupled event processing.

### Topic: `inventory.stock.updated`

**Published by:** `ImportStock`

**Subscribed by:** `InventoryStockUpdated`

**Payload:**

```go
type InventoryStockUpdatedParams struct {
    StockID   int64
    RefType   inventorydb.InventoryStockRefType
    RefID     uuid.UUID
    Change    int64
    SerialIDs []string
}
```

**Behavior on receive:**

1. If `ref_type == ProductSku`, creates serial records (bulk `COPY FROM`).
2. Updates the `stock` column (increments by `change`).
3. All operations are wrapped in a database transaction.

The pub/sub group is namespaced to `"inventory"` to scope topic routing.

---

## Models and Types

### Business Models (`model/`)

#### `inventorymodel.Stock`

```go
type Stock struct {
    ID          int64                             `json:"id"`
    RefType     inventorydb.InventoryStockRefType `json:"ref_type"`
    RefID       uuid.UUID                         `json:"ref_id"`
    Stock       int64                             `json:"stock"`
    Taken       int64                             `json:"taken"`
    DateCreated time.Time                         `json:"date_created"`
}
```

#### `inventorymodel.StockHistory`

```go
type StockHistory struct {
    ID          int64     `json:"id"`
    Change      int64     `json:"change"`
    DateCreated time.Time `json:"date_created"`
}
```

#### `inventorymodel.Serial`

```go
type Serial struct {
    ID          string                            `json:"id"`
    RefType     inventorydb.InventoryStockRefType `json:"ref_type"`
    RefID       uuid.UUID                         `json:"ref_id"`
    Status      inventorydb.InventoryStatus       `json:"status"`
    DateCreated time.Time                         `json:"date_created"`
}
```

### SQLC-Generated Enums

#### `InventoryStockRefType`

```go
const (
    InventoryStockRefTypeProductSku InventoryStockRefType = "ProductSku"
    InventoryStockRefTypePromotion  InventoryStockRefType = "Promotion"
)
```

#### `InventoryStatus`

```go
const (
    InventoryStatusActive   InventoryStatus = "Active"
    InventoryStatusInactive InventoryStatus = "Inactive"
    InventoryStatusTaken    InventoryStatus = "Taken"
    InventoryStatusDamaged  InventoryStatus = "Damaged"
)
```

Both enums implement `Scan`, `Value` (for SQL driver compatibility), and `Valid()` (for validation).

### Error Types

```go
var ErrOutOfStock = sharedmodel.NewError(
    "inventory.out_of_stock",
    "Sorry, This product (%s) is out of stock right now",
)
```

Uses `Fmt()` to interpolate the product reference ID into the message.

---
## Key Patterns

### Polymorphic References

The `(ref_type, ref_id)` pair is used across `stock` and `serial` tables to associate inventory records with different domain entities (Product SKUs, Promotions) without requiring separate tables or foreign keys per entity type.
### Optimistic Concurrency in AdjustInventory

```sql
UPDATE inventory.stock
SET stock = stock - $1, taken = taken + $1
WHERE ref_type = $2 AND ref_id = $3 AND stock >= $1;
```

The `WHERE stock >= amount` clause ensures no overselling at the database level, even if the application-level check passed on stale data.