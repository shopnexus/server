# Order Module Redesign — Full Lifecycle with Restate Workflows

**Date:** 2026-03-19
**Status:** Approved
**Scope:** Complete redesign of `internal/module/order/` — order lifecycle, refund lifecycle, disputes, store credit wallet

---

## 1. Context

ShopNexus is a Go e-commerce platform (Echo + SQLC + PostgreSQL + NATS + FX). The existing order module has partial implementations: checkout works, but refund validation is incomplete, disputes have no biz/transport layer, there is no store credit system, and order lifecycle orchestration relies on ad-hoc NATS pub/sub stubs.

This redesign replaces the entire order module with a Restate-based durable workflow architecture.

## 2. Goals

- Orchestrate the full order lifecycle (checkout -> payment -> vendor confirm -> delivery -> refund window) as a durable Restate Workflow
- Orchestrate the full refund lifecycle (create -> vendor approve/reject -> dispute -> return shipment -> credit) as a separate Restate Workflow
- Add a store credit (wallet) system backed by an append-only ledger
- Implement refund dispute resolution (customer escalates, admin resolves)
- Handle all edge cases with exactly-once semantics via Restate journaling
- Remove NATS pub/sub for order lifecycle events (Restate replaces this)

## 3. Tech Stack Additions

| Component | Technology | Version | Purpose |
|---|---|---|---|
| Workflow engine | Restate (`github.com/restatedev/sdk-go`) | v0.23.0 | Durable execution, state, promises, timers |
| Restate server | `restate-server` | 1.x (latest stable) | Invocation store, journal replay |

**Dependency:** Add `github.com/restatedev/sdk-go v0.23.0` to `go.mod` before implementation begins.

**NATS removal:** The existing `pubsub.Client` dependency in `OrderBiz` is removed. The `biz/order_pubsub.go` file is deleted. Topics `order.created`, `order.paid`, and `order.confirmed` are no longer published. No other modules currently subscribe to these topics (they were stubs), so no downstream breakage.

## 4. Architecture

```
Echo Server (:8080)
  /api/v1/order/*  -> transport/echo handlers
       |
       v
  OrderBiz (business logic, DB operations)
       |
       v
  RestateClient (ingress) -- starts workflows, resolves promises
       |
       v
Restate Server (:8081 ingress, :9070 admin)
       |
       v
Restate Handler (:9080)
  OrderWorkflow   -- durable order lifecycle orchestration
  RefundWorkflow  -- durable refund lifecycle orchestration
       |
       v
  OrderBiz (called back for DB operations, inventory, credit)
```

### Separation of Concerns

- **transport/echo**: HTTP request binding, auth, validation, calls biz + restate client
- **biz**: All business logic and DB operations. No workflow orchestration.
- **workflow**: Restate workflow definitions. Orchestrates by calling biz methods. Controls state transitions and timing.

## 5. Module File Structure

```
internal/module/order/
  fx.go                              -- FX module: wires everything

  model/
    order.go                         -- Order, OrderItem, Payment, Shipment models
    cart.go                          -- CartItem model
    refund.go                        -- Refund, RefundDispute models
    credit.go                        -- Credit (wallet) model
    error.go                         -- All domain errors

  biz/
    order.go                         -- OrderBiz struct, constructor, dependencies
    cart.go                          -- GetCart, UpdateCart, ClearCart, ListCheckoutCart
    checkout.go                      -- Checkout (creates order, delegates to workflow)
    order_query.go                   -- GetOrder, ListOrders, ListVendorOrders
    refund.go                        -- CreateRefundRecord, ListRefunds, UpdateRefundStatus
    refund_dispute.go                -- CreateDispute, ListDisputes, ResolveDispute
    credit.go                        -- GetBalance, AddCredit, UseCredit, ListCredits
    payment_map.go                   -- Payment gateway registry (COD, VNPay)
    shipment_map.go                  -- Shipment provider registry (GHTK)

  workflow/
    fx.go                            -- FX: provides Restate server + ingress client
    client.go                        -- RestateClient wrapper (start workflow, signal)
    order_workflow.go                -- OrderWorkflow struct + Run + shared handlers
    refund_workflow.go               -- RefundWorkflow struct + Run + shared handlers

  transport/echo/
    handler.go                       -- Handler struct, route registration
    cart.go                          -- Cart HTTP handlers
    order.go                         -- Order HTTP handlers
    order_vendor.go                  -- Vendor-facing handlers
    payment_webhook.go               -- VNPay IPN webhook
    refund.go                        -- Refund HTTP handlers
    refund_dispute.go                -- Dispute HTTP handlers
    credit.go                        -- Wallet/credit HTTP handlers

  db/
    migrations/
      0001_init.up.sql               -- Full schema (revised with account_credit)
      0001_init.down.sql
    queries/
      cart.sql
      order.sql
      payment.sql
      shipment.sql
      refund.sql
      refund_dispute.sql
      credit.sql
    sqlc/                            -- Generated
```

## 6. Database Schema

### Enum changes

The existing `order.status` enum is extended with two new values:

```sql
ALTER TYPE "order"."status" ADD VALUE 'Rejected';
ALTER TYPE "order"."status" ADD VALUE 'Disputed';
```

Updated enum: `Pending | Processing | Success | Canceled | Failed | Rejected | Disputed`

### Status Mapping: Restate Workflow State → DB `order.status`

The Restate workflow uses descriptive internal state names for readability. Here is how they map to the DB `order.status` column:

**Order Workflow:**

| Workflow State | DB `order.status` | Notes |
|---|---|---|
| Pending | Pending | Initial state, awaiting payment |
| Paid | Pending | Payment received, awaiting vendor confirm (still Pending in DB) |
| Processing | Processing | Vendor confirmed, shipment in progress |
| Delivered | Success | Shipment delivered, refund window open |
| Completed | Success | Refund window closed, order finalized |
| Canceled | Canceled | Vendor rejected or customer canceled |
| Failed | Failed | Payment failed/expired |

**Refund Workflow:**

| Workflow State | DB `order.status` (on refund) | Notes |
|---|---|---|
| Pending | Pending | Awaiting vendor decision |
| Rejected | Rejected | Vendor rejected, dispute window open |
| Disputed | Disputed | Customer opened dispute, admin reviewing |
| Processing | Processing | Approved, awaiting package return |
| Success | Success | Credit issued, inventory restored |
| Failed | Failed | Dispute upheld or dispute window expired |
| Canceled | Canceled | Customer canceled refund |

### Table changes

- `order.order` — add `date_delivered TIMESTAMPTZ` column (nullable, set when shipment is delivered)
- `order.cart_item` — unchanged
- `order.payment` — unchanged
- `order.shipment` — unchanged
- `order.item` — unchanged
- `order.refund` — unchanged
- `order.refund_dispute` — unchanged

### Migration files

Since the existing schema (`0001_init`) is already deployed, all changes go into a **new migration**:

```
db/migrations/
  0001_init.up.sql          -- existing (unchanged)
  0001_init.down.sql        -- existing (unchanged)
  0002_order_redesign.up.sql   -- NEW: adds account_credit, enum values, date_delivered
  0002_order_redesign.down.sql -- NEW: reverses the above
```

### New table: `order.account_credit`

```sql
CREATE TABLE "order".account_credit (
    id           BIGSERIAL PRIMARY KEY,
    account_id   UUID NOT NULL,
    amount       BIGINT NOT NULL,
    reason       TEXT NOT NULL,
    ref_id       UUID,
    date_created TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_account_credit_account ON "order".account_credit(account_id);
```

- `amount`: positive = credit added (refund), negative = credit used (checkout)
- `reason`: 'refund', 'promotion', 'manual', 'purchase'
- `ref_id`: links to refund_id or order_id
- Balance = `SUM(amount) WHERE account_id = ?`
- Append-only ledger: no UPDATEs, only INSERTs

### Concurrency safety for credit balance

To prevent negative balances from concurrent `UseCredit` calls, the `UseCredit` biz method uses a single atomic SQL statement:

```sql
INSERT INTO "order".account_credit (account_id, amount, reason, ref_id)
SELECT @account_id, @amount, @reason, @ref_id
WHERE (SELECT COALESCE(SUM(amount), 0) FROM "order".account_credit WHERE account_id = @account_id) + @amount >= 0;
```

If the `WHERE` clause fails (balance would go negative), zero rows are inserted, and the biz layer returns `ErrInsufficientCredit`. This is atomic within a single statement — no row-level locking needed.

### Refund amount calculation

`refund_amount` = `order.total` (the full order total including product cost minus discounts plus shipping). Shipping costs are included in the refund because the customer is returning the entire order.

## 7. Order Workflow

### Type

Restate Workflow, keyed by `order_id` (UUID string).

### Run Handler

```
Step 1: Save initial state
  - restate.Set(ctx, "status", "Pending")
  - restate.Set(ctx, "order_id", params.OrderID)

Step 2: Wait for payment (with timeout)
  - Race: restate.After(ctx, paymentExpiryDuration) vs
          restate.Promise[PaymentResult](ctx, "payment")
  - Use restate.Select(ctx) to race timer against promise.
  - Timer wins (payment expired):
    - saga compensate (release inventory, cancel shipment)
    - biz.UpdateOrderStatus(Failed)
    - status = Failed -> END
  - Payment signal wins:
    - On failure result: saga compensate -> status = Failed -> END
    - On success: biz.UpdatePaymentStatus(Success) -> status = Paid

Step 3: Wait for vendor confirmation
  - restate.Promise[VendorDecision](ctx, "vendor_confirm").Result()  -- SUSPEND
  - On reject: saga compensate (issue credit if paid, release inventory, cancel shipment)
    - biz.UpdateOrderStatus(Canceled) -> status = Canceled -> END
  - On confirm: biz.ConfirmOrder(vendor_id), trigger shipment
    - biz.UpdateOrderStatus(Processing) -> status = Processing

Step 4: Wait for delivery
  - restate.Promise[bool](ctx, "delivered").Result()  -- SUSPEND
  - biz.UpdateOrderStatus(Success)
  - biz.SetDeliveredDate(order_id, time.Now())
  - status = Delivered

Step 5: Refund window (7 days)
  - Race: restate.After(ctx, 7*24*time.Hour) vs
          restate.Promise[RefundRequest](ctx, "refund_requested")
  - Use restate.Select(ctx) to race timer against promise.
  - Timer wins (no refund requested):
    - status = Completed -> END
  - Refund signal wins:
    - Spawn RefundWorkflow via restate.ServiceSend(ctx, "RefundWorkflow", refund_id)
    - status = Completed -> END
  - NOTE: Refund requests arriving AFTER the timer wins are rejected at the
    API level (transport/echo checks order.date_delivered + 7d > now),
    not at the workflow level (workflow has already terminated).
```

### Checkout → Workflow Cardinality

Checkout can create N orders from a single request (one per SKU/vendor). Each order gets its **own OrderWorkflow instance** keyed by its unique `order_id`. All N workflows share a single `payment_id`, so the payment webhook resolves the payment promise on all N workflows.

### Order Cancellation via Workflow

`POST /api/v1/order/:id/cancel` works differently depending on the workflow state:
- **Waiting for payment (Step 2):** Resolves `"payment"` promise with a cancel signal (PaymentResult with status=Canceled). The workflow handles compensation.
- **Waiting for vendor confirm (Step 3):** Resolves `"vendor_confirm"` promise with a reject signal. The workflow handles compensation.
- **After vendor confirm (Steps 4-5):** Returns `ErrOrderCannotBeCanceled`. Only refund is available at this point.

### Shared Handlers

All shared handlers use `restate.WorkflowSharedContext` and explicitly resolve durable promises:

```go
func (OrderWorkflow) PaymentReceived(ctx restate.WorkflowSharedContext, result PaymentResult) error {
    return restate.Promise[PaymentResult](ctx, "payment").Resolve(result)
}

func (OrderWorkflow) VendorConfirm(ctx restate.WorkflowSharedContext, decision VendorDecision) error {
    return restate.Promise[VendorDecision](ctx, "vendor_confirm").Resolve(decision)
}

func (OrderWorkflow) Delivered(ctx restate.WorkflowSharedContext) error {
    return restate.Promise[bool](ctx, "delivered").Resolve(true)
}

func (OrderWorkflow) RefundRequested(ctx restate.WorkflowSharedContext, req RefundRequest) error {
    return restate.Promise[RefundRequest](ctx, "refund_requested").Resolve(req)
}

func (OrderWorkflow) GetStatus(ctx restate.WorkflowSharedContext) (string, error) {
    status, err := restate.Get[*string](ctx, "status")
    if err != nil || status == nil {
        return "Unknown", err
    }
    return *status, nil
}
```

### Saga Compensation

On failure after inventory is reserved:
1. Release inventory: `InventoryBiz.ReleaseInventory(serial_ids)` via `restate.Run()`
2. Cancel shipment: `shipment.Client.Cancel()` via `restate.Run()`
3. Issue store credit if payment was received: `biz.AddCredit()` via `restate.Run()`

## 8. Refund Workflow

### Type

Restate Workflow, keyed by `refund_id` (UUID string).

### Run Handler

```
Step 1: Validate and create refund record
  - Validate: order exists, belongs to customer, DB status = Success
  - Validate: order.date_delivered is set AND date_delivered + 7 days > now
  - Validate: no existing active refund for this order
  - restate.Run(ctx, biz.CreateRefundRecord)
  - restate.Set(ctx, "status", "Pending")

Step 2: Wait for vendor decision
  - restate.Promise[VendorRefundDecision](ctx, "vendor_decision").Result()  -- SUSPEND
  - APPROVED: go to Step 3
  - REJECTED:
    - restate.Run(ctx, biz.UpdateRefundStatus(Rejected, reason))
    - restate.Set(ctx, "status", "Rejected")
    - Go to Step 2b

Step 2b: Dispute window (only if rejected)
  - Race via restate.Select(ctx):
    restate.After(ctx, 7*24*time.Hour)  vs
    restate.Promise[DisputeRequest](ctx, "dispute")
  - Timer wins (no dispute):
    - restate.Run(ctx, biz.UpdateRefundStatus(Failed))
    - status = Failed -> END
  - Dispute opened:
    - restate.Run(ctx, biz.CreateDispute(reason))
    - restate.Run(ctx, biz.UpdateRefundStatus(Disputed))
    - restate.Set(ctx, "status", "Disputed")
    - Wait: restate.Promise[AdminRuling](ctx, "admin_ruling").Result()  -- SUSPEND
    - OVERRULED (customer wins):
      - restate.Run(ctx, biz.ResolveDispute(Success))
      - go to Step 3
    - UPHELD (vendor wins):
      - restate.Run(ctx, biz.ResolveDispute(Failed))
      - restate.Run(ctx, biz.UpdateRefundStatus(Failed))
      - status = Failed -> END

Step 3: Process approved refund
  - restate.Run(ctx, biz.UpdateRefundStatus(Processing))
  - restate.Set(ctx, "status", "Processing")
  - Create return shipment (PickUp: schedule pickup, DropOff: provide instructions)
  - Wait: restate.Promise[bool](ctx, "package_received").Result()  -- SUSPEND

Step 4: Complete refund
  - restate.Run(ctx): biz.RestoreInventory(order_id)
  - restate.Run(ctx): biz.AddCredit(customer_id, order.total)
  - restate.Run(ctx): biz.UpdateRefundStatus(Success)
  - restate.Set(ctx, "status", "Success")
  - END
```

### Shared Handlers

All shared handlers use `restate.WorkflowSharedContext`:

```go
func (RefundWorkflow) VendorApprove(ctx restate.WorkflowSharedContext) error {
    return restate.Promise[VendorRefundDecision](ctx, "vendor_decision").Resolve(VendorRefundDecision{Approved: true})
}

func (RefundWorkflow) VendorReject(ctx restate.WorkflowSharedContext, reason string) error {
    return restate.Promise[VendorRefundDecision](ctx, "vendor_decision").Resolve(VendorRefundDecision{Approved: false, Reason: reason})
}

func (RefundWorkflow) OpenDispute(ctx restate.WorkflowSharedContext, reason string) error {
    return restate.Promise[DisputeRequest](ctx, "dispute").Resolve(DisputeRequest{Reason: reason})
}

func (RefundWorkflow) ResolveDispute(ctx restate.WorkflowSharedContext, ruling AdminRuling) error {
    return restate.Promise[AdminRuling](ctx, "admin_ruling").Resolve(ruling)
}

func (RefundWorkflow) PackageReceived(ctx restate.WorkflowSharedContext) error {
    return restate.Promise[bool](ctx, "package_received").Resolve(true)
}

func (RefundWorkflow) GetStatus(ctx restate.WorkflowSharedContext) (string, error) {
    status, err := restate.Get[*string](ctx, "status")
    if err != nil || status == nil {
        return "Unknown", err
    }
    return *status, nil
}
```

### Refund State Machine

```
Pending --> Processing --> Success
  |              ^
  v              |
Rejected --> Disputed --> Processing (overruled)
  |              |
  v              v
 END         Failed --> END (upheld)
```

Uses DB enum values: `Pending | Processing | Success | Canceled | Failed | Rejected | Disputed`

## 9. HTTP API

### Cart

| Method | Route | Auth | Description |
|---|---|---|---|
| GET | /api/v1/order/cart | Customer | Get cart items |
| POST | /api/v1/order/cart | Customer | Add/update cart item |
| DELETE | /api/v1/order/cart | Customer | Clear cart |
| GET | /api/v1/order/cart-checkout | Customer | Preview checkout |

### Order

| Method | Route | Auth | Description |
|---|---|---|---|
| POST | /api/v1/order/checkout | Customer | Checkout, starts OrderWorkflow |
| POST | /api/v1/order/quote | Customer | Get quotes (no purchase) |
| GET | /api/v1/order | Customer | List my orders |
| GET | /api/v1/order/:id | Customer | Get order detail |
| GET | /api/v1/order/:id/status | Customer | Get workflow status |
| POST | /api/v1/order/:id/cancel | Customer | Cancel order |

### Vendor

| Method | Route | Auth | Description |
|---|---|---|---|
| GET | /api/v1/order/vendor | Vendor | List vendor orders |
| POST | /api/v1/order/vendor/:id/confirm | Vendor | Confirm order |
| POST | /api/v1/order/vendor/:id/reject | Vendor | Reject order |
| POST | /api/v1/order/vendor/:id/delivered | Vendor | Mark delivered |

### Payment Webhook

| Method | Route | Auth | Description |
|---|---|---|---|
| GET | /api/v1/order/ipn | HMAC | VNPay IPN callback |

### Refund

| Method | Route | Auth | Description |
|---|---|---|---|
| POST | /api/v1/order/refund | Customer | Create refund, starts RefundWorkflow |
| GET | /api/v1/order/refund | Customer | List my refunds |
| GET | /api/v1/order/refund/:id | Customer | Get refund detail |
| GET | /api/v1/order/refund/:id/status | Customer | Get workflow status |
| DELETE | /api/v1/order/refund/:id | Customer | Cancel refund |

### Vendor Refund

| Method | Route | Auth | Description |
|---|---|---|---|
| POST | /api/v1/order/vendor/refund/:id/approve | Vendor | Approve refund |
| POST | /api/v1/order/vendor/refund/:id/reject | Vendor | Reject refund |
| POST | /api/v1/order/vendor/refund/:id/received | Vendor | Package received |

### Disputes

| Method | Route | Auth | Description |
|---|---|---|---|
| POST | /api/v1/order/refund/:id/dispute | Customer | Open dispute |
| GET | /api/v1/order/dispute | Admin | List open disputes |
| POST | /api/v1/order/dispute/:id/resolve | Admin | Resolve dispute |

### Wallet / Credit

| Method | Route | Auth | Description |
|---|---|---|---|
| GET | /api/v1/order/credit/balance | Customer | Get balance |
| GET | /api/v1/order/credit/history | Customer | List transactions |

## 10. Domain Errors

Uses the existing `sharedmodel.NewError(code, message)` pattern with dot-separated codes:

```go
var (
    // Order
    ErrOrderNotFound          = sharedmodel.NewError("order.not_found", "Order not found")
    ErrOrderNotDelivered      = sharedmodel.NewError("order.not_delivered", "Order must be delivered before requesting a refund")
    ErrOrderAlreadyCanceled   = sharedmodel.NewError("order.already_canceled", "Order is already canceled")
    ErrOrderCannotBeCanceled  = sharedmodel.NewError("order.cannot_cancel", "Order cannot be canceled at this stage")
    ErrOrderItemNotFound      = sharedmodel.NewError("order.order_item_not_found", "Sorry, we couldn't find the item you requested")

    // Payment
    ErrPaymentGatewayNotFound = sharedmodel.NewError("order.payment_gateway_not_found", "Sorry, we couldn't find the payment gateway you requested")
    ErrPaymentFailed          = sharedmodel.NewError("order.payment_failed", "Payment failed")
    ErrPaymentExpired         = sharedmodel.NewError("order.payment_expired", "Payment has expired")

    // Refund
    ErrRefundWindowExpired    = sharedmodel.NewError("order.refund_window_expired", "Refund window (7 days) has expired")
    ErrRefundAlreadyExists    = sharedmodel.NewError("order.refund_already_exists", "An active refund already exists for this order")
    ErrRefundAddressRequired  = sharedmodel.NewError("order.refund_address_required", "Address is required for pick up method")
    ErrRefundNotRejected      = sharedmodel.NewError("order.refund_not_rejected", "Can only dispute rejected refunds")
    ErrRefundCannotBeCanceled = sharedmodel.NewError("order.refund_cannot_cancel", "Refund cannot be canceled at this stage")

    // Dispute
    ErrDisputeWindowExpired   = sharedmodel.NewError("order.dispute_window_expired", "Dispute window (7 days) has expired")
    ErrDisputeAlreadyExists   = sharedmodel.NewError("order.dispute_already_exists", "A dispute already exists for this refund")

    // Credit
    ErrInsufficientCredit     = sharedmodel.NewError("order.insufficient_credit", "Insufficient store credit")
)
```

## 11. Cross-Module Dependencies

OrderBiz depends on:
- `AccountBiz` — GetDefaultContact() for vendor addresses
- `CatalogBiz` — ListProductSku(), ListProductSpu()
- `InventoryBiz` — ReserveInventory(), ReleaseInventory()
- `PromotionBiz` — CalculatePromotedPrices()
- `CommonBiz` — GetResources(), UpdateResources(), UpdateServiceOptions()
- `AnalyticBiz` — TrackInteraction() for purchase/cancel/refund events

Workflow handlers depend on:
- `OrderBiz` — all DB operations and business logic

## 12. Infrastructure Requirements

### Restate Server

- Single Rust binary, no external dependencies (embedded RocksDB)
- Version: 1.x (latest stable, compatible with Go SDK v0.23.0)
- Ports: 8081 (ingress), 9070 (admin), 9071 (discovery)
- Start: `restate-server` or via Docker (`docker.restate.dev/restatedev/restate:latest`)
- Register service: `restate deployments register http://localhost:9080`

### Restate Handler

- Go HTTP server on port 9080
- Uses `github.com/restatedev/sdk-go/server`
- Registered as FX lifecycle hook (start on app boot, register with Restate server)

### RestateClient (workflow/client.go)

The `RestateClient` wraps the Restate ingress HTTP client for starting workflows and sending signals:

```go
type RestateClient struct {
    ingressURL string  // e.g., "http://localhost:8081"
}

// StartOrderWorkflow starts a new OrderWorkflow instance
func (c *RestateClient) StartOrderWorkflow(ctx context.Context, orderID string, params CheckoutResult) error {
    // POST to http://localhost:8081/OrderWorkflow/{orderID}/Run
    // Body: JSON-encoded CheckoutResult
}

// SignalOrderWorkflow sends a signal to a running OrderWorkflow instance
func (c *RestateClient) SignalOrderWorkflow(ctx context.Context, orderID string, handler string, payload any) error {
    // POST to http://localhost:8081/OrderWorkflow/{orderID}/{handler}
    // Body: JSON-encoded payload
}

// StartRefundWorkflow starts a new RefundWorkflow instance
func (c *RestateClient) StartRefundWorkflow(ctx context.Context, refundID string, params RefundParams) error {
    // POST to http://localhost:8081/RefundWorkflow/{refundID}/Run
}

// SignalRefundWorkflow sends a signal to a running RefundWorkflow instance
func (c *RestateClient) SignalRefundWorkflow(ctx context.Context, refundID string, handler string, payload any) error {
    // POST to http://localhost:8081/RefundWorkflow/{refundID}/{handler}
}

// GetWorkflowStatus queries the current status of a workflow
func (c *RestateClient) GetWorkflowStatus(ctx context.Context, workflowName, key string) (string, error) {
    // POST to http://localhost:8081/{workflowName}/{key}/GetStatus
}
```

Uses raw HTTP calls to the Restate ingress endpoint (port 8081). This is simpler than importing the `ingress` package and avoids coupling to SDK internals.

### Testing

The Restate Go SDK provides a testing package at `github.com/restatedev/sdk-go/testing` with mock contexts. If this package is unavailable or insufficient, use integration tests with a real Restate server started via Docker in CI.

## 13. Refund Business Rules

1. Only full order refunds (no partial item refunds)
2. Most orders have 1 SKU; multi-item only for buy-X-get-Y promotions
3. Refund window: 7 days after delivery
4. Money returned as store credit (append-only ledger)
5. Refund methods: PickUp (vendor arranges) or DropOff (customer ships)
6. Vendor must approve/reject refund
7. Rejected refunds can be disputed within 7 days
8. Admin resolves disputes (overrule or uphold vendor decision)
9. Inventory is restored after package is received and verified

## 14. Testing Strategy

| Layer | Approach |
|---|---|
| Biz layer | Unit tests with mocked SQLC Querier interface |
| Workflow | Integration tests with real Restate server (Docker). Use `github.com/restatedev/sdk-go/testing` if available, otherwise direct HTTP calls to ingress. |
| Transport | HTTP handler tests with httptest + mocked OrderBiz |
| Integration | Real Restate server + PostgreSQL, full flow tests |

## 15. Breaking Changes

This is a full module redesign. The following are breaking API changes:

| Old | New | Impact |
|---|---|---|
| `POST /api/v1/order/confirm` (body-based ID) | `POST /api/v1/order/vendor/:id/confirm` (URL param) | Vendor clients must update |
| No reject endpoint | `POST /api/v1/order/vendor/:id/reject` | New endpoint |
| No delivered endpoint | `POST /api/v1/order/vendor/:id/delivered` | New endpoint |
| `PATCH /api/v1/order/refund` (body-based) | `DELETE /api/v1/order/refund/:id` (URL param) | Refund cancel via DELETE |
| `POST /api/v1/order/refund/confirm` (body-based) | Vendor approve/reject via URL params | Vendor refund endpoints restructured |
| NATS `order.created` / `order.paid` topics | Removed (Restate replaces) | No downstream subscribers existed (stubs) |

## 16. Store Credit Usage at Checkout (Future)

v1 only tracks credit balance and issues credits via refund. Applying store credit during checkout (subtracting from order total) is out of scope for this design and will be a separate feature. The `UseCredit` biz method and `ErrInsufficientCredit` error are defined for future use.

## 17. Multi-Vendor Order Note

Each checkout creates one order per SKU/vendor. The 7-day refund window runs independently per order (each order has its own `date_delivered` timestamp and its own OrderWorkflow). Refund requests are per-order, not per-checkout-session.
