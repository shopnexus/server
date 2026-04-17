# Restate Checkout Saga Integration

**Date:** 2026-03-17
**Status:** Approved
**Scope:** Integrate Restate durable execution for the checkout flow, replacing the monolithic DB transaction with a saga pattern that compensates cross-service calls.

## Context

The current checkout flow (`internal/module/order/biz/checkout.go`) runs as a single Postgres transaction spanning cart removal, inventory reservation, shipment creation, price calculation, payment processing, and order creation. External HTTP calls (GHTK shipment, VNPay payment) happen inside this transaction, creating orphaned external state if the transaction rolls back.

The project is moving toward a gRPC microservice architecture where each module (order, inventory, shipment, promotion, payment, catalog, account, etc.) becomes a separate service with its own database. Cross-service calls cannot share DB transactions.

## Architecture

### Three-Process Model

```
Echo API Server (cmd/server, existing)
  |  POST /checkout triggers workflow via ingress.Client
  v
Restate Server (Docker container)
  |  Durable execution engine: journals state, retries, compensations
  |  Ports: 8080 (ingress), 9070 (admin), 9071 (Web UI)
  v
Restate Handler Service (cmd/restate, new Go binary)
  |  CheckoutWorkflow.Run() -- saga with compensations
  |  Calls biz layers directly (in-process for now, gRPC later)
  v
Biz Layers (inventory, shipment, promotion, payment, order)
```

- **Echo API** no longer calls `OrderBiz.Checkout()` directly for checkout. It sends a request to the Restate server via `ingress.Client`, which orchestrates the workflow.
- **Restate server** runs as a Docker container. It stores workflow execution journals, manages retries with exponential backoff, and guarantees exactly-once step execution.
- **Restate handler service** is a new Go binary (`cmd/restate/`) that registers workflow handlers. It calls existing biz layer functions directly (in-process). When services are split into gRPC microservices later, these direct calls will be swapped to gRPC client calls by the developer.

### Relationship to NATS

Restate replaces NATS for orchestrated workflows (checkout saga, order lifecycle). NATS stays for fanout-style events (analytics interactions, notifications) where fire-and-forget delivery is appropriate.

## Checkout Saga Flow

`CheckoutWorkflow.Run(ctx restate.WorkflowContext, req CheckoutRequest) (CheckoutResult, error)`

Note: `WorkflowContext` (not `Context`) is required for workflow `Run` handlers. The return type includes `CheckoutResult` so the Echo handler can retrieve order IDs and redirect URLs.

### Steps

| # | Action | Service | Compensation | Cross-service? |
|---|--------|---------|-------------|---------------|
| 1 | Validate request, resolve SKU/SPU data, resolve vendor contacts | Catalog + Account (read-only) | None | Yes (read) |
| 2 | Reserve inventory (lock stock, claim serials) | Inventory | `ReleaseInventory(reservationID)` | Yes |
| 3 | Create shipment (external provider call + DB record) | Shipment | `CancelShipment(shipmentID)` — cancels with external provider (e.g. GHTK API) AND updates DB record status to Cancelled | Yes |
| 4 | Calculate promoted prices | Promotion | None (read-only) | Yes (read) |
| 5 | Create payment order (gateway call + DB record) | Payment | `CancelPayment(paymentID)` | Yes |
| 6 | Create order (DB tx: remove cart items + create order + create order items) | Order | `CancelOrder(orderID)` | No (local) |

### Compensation Behavior

- Compensations are tracked in a `[]func() (restate.Void, error)` slice.
- A `defer` block runs compensations in reverse order (LIFO) if any step returns an error.
- Each compensation is itself a `restate.Run()` call, making it durable and retryable.
- Two-phase pattern: register compensation **after** the action succeeds (because the compensation needs the returned ID).

### Failure Scenarios

| Failure at step | Compensations executed |
|----------------|----------------------|
| Step 2 (inventory) | None |
| Step 3 (shipment) | Release inventory |
| Step 4 (promotion) | Cancel shipment, release inventory |
| Step 5 (payment) | Cancel shipment, release inventory |
| Step 6 (order) | Cancel payment, cancel shipment, release inventory |

### Error Handling

- Transient errors (network, temporary DB failures): Restate retries with exponential backoff automatically.
- Business logic failures (out of stock, invalid promo code): Return `restate.TerminalError()` to stop retries and trigger compensations.
- Each `restate.Run()` step can be named via `restate.WithName()` for observability. Retry behavior is configured at the Restate server level (exponential backoff by default).

## Code Structure

### New Files

| File | Purpose |
|------|---------|
| `cmd/restate/main.go` | Restate handler server entrypoint. Wires biz layer dependencies, registers `CheckoutWorkflow`, starts HTTP server on handler port. |
| `internal/module/order/workflow/checkout.go` | `CheckoutWorkflow` struct with `Run()` handler implementing the saga pattern. |
| `internal/module/order/workflow/checkout_model.go` | `CheckoutRequest` and `CheckoutResult` types for the workflow. |

### Modified Files

| File | Change |
|------|--------|
| `internal/module/inventory/biz/inventory.go` | Add `ReleaseInventory()` function -- reverses `ReserveInventory()` by restoring stock and setting serials back to Available. |
| `internal/module/order/transport/echo/` | Checkout HTTP handler changes from calling `OrderBiz.Checkout()` to triggering workflow via `ingress.Client`. |
| `config/struct.go` | Add `RestateConfig` struct with `URL` (ingress endpoint) and `HandlerPort` fields. |
| `deployment/docker-compose.yml` | Add `restate` server container and `restate-handler` service container. |
| `go.mod` | Add `github.com/restatedev/sdk-go` dependency. |
| `internal/app/` (fx wiring) | Provide `*ingress.Client` via `ingress.NewClient(cfg.Restate.URL)`. |

### Not Creating

- No gRPC stubs or client interfaces. Workflow handlers call biz layers directly. Developer will swap to gRPC calls when services are split.
- No wrapper around `ingress.Client`. The SDK's client is injected directly via fx.

## Dependency Injection

### Restate Ingress Client (in Echo server)

```go
// fx provider
fx.Provide(func(cfg *config.Config) *ingress.Client {
    return ingress.NewClient(cfg.Restate.URL)
})
```

Import: `ingress "github.com/restatedev/sdk-go/client/ingress"` (or the correct subpackage path from the SDK).

Injected into `OrderHandler` as `*ingress.Client`. The handler uses the SDK's generic `ingress.Workflow[I, O]()` function to trigger workflows -- domain-specific call stays in the order module.

### Restate Handler Server (cmd/restate)

The handler server wires its own biz layer dependencies. It can use fx (like the main server) or manual wiring. The `CheckoutWorkflow` struct receives biz layer instances:

```go
type CheckoutWorkflow struct {
    order     *orderbiz.OrderBiz
    inventory *inventorybiz.InventoryBiz
    catalog   *catalogbiz.CatalogBiz
    promotion *promotionbiz.PromotionBiz
    account   *accountbiz.AccountBiz
    // shipment and payment maps: either exposed from OrderBiz or injected directly
    shipmentMap map[string]shipment.Client
    paymentMap  map[string]payment.Client
}
```

**Note on `Storage` parameter:** Biz functions like `ReserveInventory` currently accept a `Storage` parameter for cross-module transaction joining. When called from the Restate workflow, `Storage` should be passed as `nil` so the function uses its own internal DB connection and transaction. This applies to any biz function that accepts a `Storage` or `preferStorage` parameter.

## Deployment

### Docker Compose Additions

A 3-node Restate cluster already exists in `deployment/docker-compose.yml` (`restate-1`, `restate-2`, `restate-3`) with replication factor 2 and MinIO-backed snapshots. The ingress is exposed on port `8080` (restate-1), admin on `9470`.

The only new addition needed is the handler service:

```yaml
restate-handler:
  build:
    context: .
    dockerfile: cmd/restate/Dockerfile
  ports:
    - "9080:9080"
  depends_on:
    - restate-1
    - postgres
    - redis
  environment:
    - RESTATE_URL=http://restate-1:8080
```

### Service Registration

After both containers start, register the handler with the Restate server:

```bash
curl -X POST http://localhost:9470/deployments \
  -H 'content-type: application/json' \
  -d '{"uri": "http://restate-handler:9080"}'
```

This can be automated via a healthcheck-triggered init script or a startup hook in the handler binary.

## Configuration

Add to `config/struct.go`:

```go
type RestateConfig struct {
    URL         string `mapstructure:"url"`          // "http://restate:8080"
    HandlerPort string `mapstructure:"handler_port"` // ":9080"
}
```

Add to config file (e.g., `config.yaml`):

```yaml
restate:
  url: "http://restate:8080"
  handler_port: ":9080"
```

## New Function: ReleaseInventory

The `InventoryBiz` currently has no function to reverse a reservation. This is needed as the compensation for step 2.

**Behavior:**
- Accept a reservation identifier (list of SKU IDs + quantities, or serial IDs)
- Increment stock counts back
- Set serial statuses from `Taken` back to `Available`
- Runs within its own DB transaction (internal to inventory service)

This mirrors `ReserveInventory` in reverse.

## Migration Path

1. **Phase 1 (this spec):** Restate handler calls biz layers directly (in-process). Both the Echo server and the Restate handler share the same Postgres database.
2. **Phase 2 (future):** Split modules into separate gRPC services with their own databases. Swap direct biz calls in the workflow to gRPC client calls. The saga structure and compensations remain unchanged.

## Restate SDK Reference

| Function | Purpose |
|----------|---------|
| `restate.Run[T](ctx, fn, opts...)` | Durable side effect -- result journaled, not re-executed on replay |
| `restate.Void` | Return type for void Run calls |
| `restate.WithName("label")` | Name a Run step for observability |
| `restate.TerminalError(err, code)` | Stop retries, propagate error, trigger compensations |
| `restate.UUID(ctx)` | Deterministic UUID generation |
| `server.NewRestate().Bind(restate.Reflect(s)).Start(ctx, addr)` | Start handler server (blocks until context cancelled) |
| `ingress.NewClient(url)` | Create external client (`import "github.com/restatedev/sdk-go/client/ingress"`) |
| `ingress.Workflow[I,O](client, svc, id, handler).Request(ctx, input)` | Trigger workflow synchronously |
| `ingress.Workflow[I,O](client, svc, id, handler).Send(ctx, input)` | Trigger workflow async (fire-and-forget) |
