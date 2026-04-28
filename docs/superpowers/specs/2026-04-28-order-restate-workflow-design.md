# Order Module — Restate Workflow Refactor

**Date**: 2026-04-28
**Status**: Design draft, not implemented.
**Supersedes**: `2026-04-27-order-lifecycle-workflow-design.md` (single-workflow approach replaced by 3-workflow split).

---

## 1. Goal

Refactor the order module from the current `Service` + scattered DB-status-machine pattern to **three Restate `Workflow` services**, each owning one phase of the order lifecycle:

| Workflow           | ID =                  | Lifetime   | Owns                                                                              |
| ------------------ | --------------------- | ---------- | --------------------------------------------------------------------------------- |
| `CheckoutWorkflow` | `checkout_session_id` | minutes    | reserve inventory, create payment_session/txs/items, wait for buyer payment       |
| `ConfirmWorkflow`  | `confirm_session_id`  | minutes    | quote aggregate transport, create order.order, wait for seller confirm-fee payment |
| `PayoutWorkflow`   | `order_id`            | days–weeks | hold escrow 7d, watch refund state, release or cancel payout                      |

The workflow `Run` function is the source of truth for phase transitions. Side-effect rollback is handled by an in-workflow LIFO **saga compensator** stack (no DB log; relies on Restate journal replay).

The legacy `OrderHandler` Service binding is preserved for non-workflow methods: refund (2-stage seller flow), dispute, cart, list/get, dashboard, transport_update, escrow utilities.

---

## 2. Why three workflows, not one

The 2026-04-27 spec proposed one workflow per payment_session. Splitting into three is preferred because:

1. **Lifetime mismatch.** Checkout (~minutes) and Payout (~days) have radically different journal/state shapes. Combining forces the long-running workflow to carry checkout's history forever.
2. **Hot-fix safety.** A bug in payout/refund logic should not require migrating an in-flight checkout's journal. Smaller workflows = shorter journals = fewer compatibility traps on deploy.
3. **Boundary clarity.** Each workflow boundary maps to a business commit point: "buyer paid", "seller accepted + paid confirm fee", "money held in escrow".
4. **Refund concurrency simplification** (decided in Q4): refund-per-order means PayoutWorkflow only needs a boolean snapshot of refund state, not per-item tracking — but it still benefits from being isolated from confirm-time journal.

---

## 3. Architecture

```text
┌─────────────────────────────────────────────────────────────────┐
│                        HTTP Transport                            │
│  POST /buyer/checkout    POST /seller/pending/confirm           │
│  POST /buyer/checkout/:id/cancel                                │
│  Webhook: VNPay IPN, COD, transport providers                   │
└─────────────┬───────────────────┬──────────────────┬────────────┘
              │ Send + Attach     │ Send + Attach    │ Send (signal)
              ▼                   ▼                  ▼
   ┌──────────────────┐  ┌──────────────────┐  ┌──────────────┐
   │ CheckoutWorkflow │  │ ConfirmWorkflow  │  │   Order      │
   │  {sessionID}     │  │  {confirmSessID} │  │  (Service)   │
   │  ├─ Run          │  │  ├─ Run          │  │  refund/...  │
   │  ├─ WaitPayURL   │  │  ├─ WaitPayURL   │  │  dispute/... │
   │  ├─ ResolvePay   │  │  ├─ ResolvePay   │  │  OnPaymentR. │
   │  └─ Cancel       │  │  └─ Cancel       │  │  OnTransp.R. │
   └──────────────────┘  └──────────┬───────┘  └──────┬───────┘
       (vài phút)                   │                 │
                                    │ Send orderID    │ Send refundID
                                    ▼                 ▼
                          ┌──────────────────────────────────┐
                          │       PayoutWorkflow             │
                          │       {orderID}                  │
                          │       ├─ Run (chạy 7d+)          │
                          │       └─ OnRefundChanged         │
                          └──────────────────────────────────┘
                              (escrow timer + refund watcher)
```

Cross-module dependencies (unchanged): Account (wallet, contact, notification), Inventory (reserve/release), Catalog (SKU/SPU), Common (FX, country), Promotion (price calc).

---

## 4. SDK constraints (Restate Go v0.24)

- `restate.Select` is **deprecated**; use `restate.WaitFirst(ctx, futs...) (Future, error)`. Dispatch via interface/pointer equality against the saved future handle.
- `WorkflowContext` has exactly one `Run` method per handler struct. All other handlers on the same struct must accept `WorkflowSharedContext`.
- `restate.Promise[T](ctx, name)` is workflow-scoped (key = workflow ID + name); `Resolve` is idempotent-with-error on repeated calls.
- Workflow ID is single-use — cannot be reinvoked after completion.
- Reflection still via `restate.Reflect(handler)` + `Bind(...)` in `internal/app/restate.go`.

Memory ref: `reference_restate_sdk_go_v024.md`.

---

## 5. Code organization

| Service binding             | Struct                    | File(s)                          | Methods                                                                                                                                                                                          |
| --------------------------- | ------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `Order` (Service, existing) | `OrderHandler`            | `biz/*.go` (existing)            | refund, dispute, cart, list, dashboard, escrow utilities, transport_update, **`OnPaymentResult`** (new), **`OnTransportResult`** (existing as `UpdateTransportStatus` — may rename)              |
| `CheckoutWorkflow`          | `CheckoutWorkflowHandler` | `biz/checkout_workflow.go` (new) | `Run`, `WaitPaymentURL`, `ResolvePayment`, `CancelCheckout`                                                                                                                                      |
| `ConfirmWorkflow`           | `ConfirmWorkflowHandler`  | `biz/confirm_workflow.go` (new)  | `Run`, `WaitPaymentURL`, `ResolvePayment`, `CancelConfirm`                                                                                                                                       |
| `PayoutWorkflow`            | `PayoutWorkflowHandler`   | `biz/payout_workflow.go` (new)   | `Run`, `OnRefundChanged`                                                                                                                                                                         |

All workflow handlers share dependencies with `OrderHandler` via constructor injection (storage, account client, transport map, payment map, common client). Bind 4 services in `internal/app/restate.go`.

The legacy `BuyerCheckout` and `ConfirmSellerPending` methods on `OrderHandler` are **removed** (logic moves into the workflow Run). HTTP transport layer is updated accordingly.

---

## 6. Saga compensator helper

Shared by all 3 workflow Run functions:

```go
// internal/module/order/biz/saga.go
type Saga struct {
    ctx          restate.WorkflowContext
    compensators []Compensator // LIFO
}

type Compensator struct {
    Name string
    Fn   func(restate.RunContext) error
}

// Defer appends BEFORE the action it compensates.
func (s *Saga) Defer(name string, fn func(restate.RunContext) error) {
    s.compensators = append(s.compensators, Compensator{Name: name, Fn: fn})
}

// Compensate runs LIFO; each compensator wrapped in restate.RunVoid → infinite retry.
func (s *Saga) Compensate() {
    for i := len(s.compensators) - 1; i >= 0; i-- {
        c := s.compensators[i]
        _ = restate.RunVoid(s.ctx, func(rctx restate.RunContext) error {
            return c.Fn(rctx)
        }, restate.WithName("compensate:"+c.Name))
    }
}

// Clear is called on the success exit path.
func (s *Saga) Clear() { s.compensators = nil }
```

**Replay semantics**: the `compensators []Compensator` slice lives only in Go memory and is rebuilt on workflow replay. Because every action between `Defer()` calls is wrapped in `restate.Run`, Restate's journal replay returns cached results without re-executing — so the slice rebuilds deterministically to the exact same state at the crash point. No DB log is needed for this to work.

**When fired** (decided Q5):
- Run() returns terminal error → Compensate
- `cancel_signal` resolves the event promise → Compensate
- `expire_timer` wins WaitFirst → Compensate
- "paid" event → `Clear()` → no compensation

**Failure handling** (decided Q5): each compensator runs inside `restate.RunVoid`, so Restate retries with exponential backoff indefinitely. Compensators must be idempotent (release inventory, credit wallet, restore cart, mark session failed — all idempotent in current account/inventory module APIs).

---

## 7. CheckoutWorkflow

### 7.1 Run flow

```go
func (h *CheckoutWorkflowHandler) Run(ctx restate.WorkflowContext, input CheckoutInput) (out CheckoutOutput, err error) {
    saga := NewSaga(ctx)
    var (
        cancelled bool
        expired   bool
    )
    defer func() {
        if err != nil || cancelled || expired {
            saga.Compensate()
        }
    }()

    // 1. validate input
    // 2. load buyer profile, country guard
    // 3. fetch SKUs + SPUs
    // 4. snapshot FX rate buyer→seller
    //    (all wrapped in restate.Run / restate.RunAsync)

    // 5. remove from cart
    saga.Defer("restore_cart", restoreCartFn)
    if err = restate.RunVoid(ctx, removeCartFn); err != nil { return }

    // 6. reserve inventory
    saga.Defer("release_inventory", releaseInventoryFn)
    if _, err = inventory.ReserveInventory(...); err != nil { return }

    // 7. quote transport per item
    // 8. compute totals (subtotal + transport per item, in buyer currency)
    // 9. wallet/gateway split

    // 10. create payment_session + N txs + N order.items (one restate.Run)
    saga.Defer("mark_session_failed_and_credit", compensateSessionFn)
    created, err := restate.Run(ctx, createSessionTxItemsFn)
    if err != nil { return }

    // 11. wallet debit (cross-module) + MarkTxSuccess (auto-promotes session if no gateway leg)
    if walletAmount > 0 {
        saga.Defer("credit_wallet", creditWalletFn)
        _ = restate.RunVoid(ctx, walletDebitFn)
        _ = restate.RunVoid(ctx, markTxSuccessFn)
    }

    // 12. paymentClient.Create() → URL, persist on tx.data, resolve payment_url promise
    if gatewayAmount > 0 {
        url, err := restate.Run(ctx, paymentCreateFn)
        if err != nil { return out, err }
        _ = restate.RunVoid(ctx, persistGatewayURLFn)
        _ = restate.Promise[string](ctx, "payment_url").Resolve(url)
    } else {
        // Wallet-only happy path: resolve URL promise empty so WaitPaymentURL unblocks immediately.
        _ = restate.Promise[string](ctx, "payment_url").Resolve("")
    }

    // 13. wait for outcome
    eventFut := restate.Promise[PaymentEvent](ctx, "payment_event").Future()
    expiryFut := restate.After(ctx, paymentExpiry)
    done, _ := restate.WaitFirst(ctx, eventFut, expiryFut)

    switch done {
    case eventFut:
        ev, _ := eventFut.Result()
        switch ev.Kind {
        case "paid":
            saga.Clear()
            // notify seller (fire-and-forget restate.ServiceSend Account.CreateNotification)
            // fire-and-forget Analytic.CreateInteraction (purchase event)
            return CheckoutOutput{Status: "paid", SessionID: input.SessionID}, nil
        case "cancelled":
            cancelled = true
            return CheckoutOutput{Status: "cancelled"}, nil
        case "failed":
            return out, ordermodel.ErrPaymentFailed.Terminal()
        }
    case expiryFut:
        expired = true
        return CheckoutOutput{Status: "expired"}, nil
    }
    return
}
```

For wallet-only checkouts (no gateway leg), `payment_event` is resolved internally right after step 11 — Run() doesn't actually wait. The same code path handles it.

### 7.2 Shared handlers

```go
func (h *CheckoutWorkflowHandler) WaitPaymentURL(ctx restate.WorkflowSharedContext, _ struct{}) (string, error) {
    return restate.Promise[string](ctx, "payment_url").Result()
}

func (h *CheckoutWorkflowHandler) ResolvePayment(ctx restate.WorkflowSharedContext, ev PaymentEvent) error {
    return restate.Promise[PaymentEvent](ctx, "payment_event").Resolve(ev)
}

func (h *CheckoutWorkflowHandler) CancelCheckout(ctx restate.WorkflowSharedContext, _ struct{}) error {
    return restate.Promise[PaymentEvent](ctx, "payment_event").Resolve(PaymentEvent{Kind: "cancelled"})
}
```

`PaymentEvent` is a discriminated union:

```go
type PaymentEvent struct {
    Kind   string // "paid" | "failed" | "cancelled"
    TxID   int64  // for "paid" / "failed"
    Reason string // for "failed"
}
```

---

## 8. ConfirmWorkflow

Identical structure to CheckoutWorkflow with these differences:

- **Pre-check**: every `item.payment_session_id` referenced by `params.ItemIDs` must have status = Success. Fail terminal otherwise.
- **No inventory reserve / cart compensator** — items already exist (created in CheckoutWorkflow). Compensators are seller-side only (mark confirm session failed, credit seller wallet if confirm-fee wallet leg debited).
- **After payment paid**, instead of just notifying:
  1. Create `order.transport` row (via existing transport client).
  2. Create `order.order` row.
  3. Set `order_item.order_id` for all confirmed items.
  4. Notify buyer "items confirmed".
  5. **Submit PayoutWorkflow**:
     ```go
     restate.Workflow[any](ctx, "PayoutWorkflow", orderID.String(), "Run").
         Send(PayoutInput{OrderID: orderID, SellerID: sellerID, PaidTotal: paidTotal})
     ```
  6. Return.
- **Compensators on failure** still need to handle the case where these post-payment side effects partially succeeded — they all happen inside one final `restate.Run` for atomicity, so either all-or-none.

---

## 9. PayoutWorkflow

### 9.1 Run flow

```go
func (h *PayoutWorkflowHandler) Run(ctx restate.WorkflowContext, input PayoutInput) (PayoutOutput, error) {
    // Step 1: create payout payment_session + payout tx (Pending), durable.
    if err := restate.RunVoid(ctx, createPayoutSessionFn); err != nil { return out, err }

    // Step 2: loop until decision.
    iter := 0
    deadline := time.Now().Add(escrowHold) // 7d
    for {
        // Reload refund snapshot fresh each iteration.
        snapshot, err := restate.Run(ctx, getRefundSnapshotFn(input.OrderID))
        if err != nil { return out, err }

        switch {
        case snapshot.LastRefundApproved:
            // Refund won → cancel payout, exit.
            _ = restate.RunVoid(ctx, cancelPayoutSessionFn)
            // notify seller
            return PayoutOutput{Outcome: "refunded"}, nil

        case !snapshot.HasActiveRefund && time.Now().After(deadline):
            // Escrow window passed, no active refund → release.
            _ = restate.RunVoid(ctx, markPayoutSessionSuccessFn)
            _ = restate.RunVoid(ctx, creditSellerWalletFn)
            // notify seller
            return PayoutOutput{Outcome: "released"}, nil
        }

        // Otherwise wait for: next refund signal OR deadline (whichever sooner).
        iter++
        signalFut := restate.Promise[any](ctx, fmt.Sprintf("refund_changed_%d", iter)).Future()
        var deadlineFut restate.Future
        if remaining := time.Until(deadline); remaining > 0 {
            deadlineFut = restate.After(ctx, remaining).Future()
        }
        // Save current iter so OnRefundChanged knows which promise to resolve.
        _ = restate.Set(ctx, "refund_iter", iter)

        if deadlineFut != nil {
            _, _ = restate.WaitFirst(ctx, signalFut, deadlineFut)
        } else {
            _, _ = restate.WaitFirst(ctx, signalFut)
        }
        // loop continues; next iteration re-evaluates snapshot.
    }
}
```

### 9.2 Shared handler

```go
func (h *PayoutWorkflowHandler) OnRefundChanged(ctx restate.WorkflowSharedContext, _ struct{}) error {
    iter, _ := restate.Get[int](ctx, "refund_iter")
    return restate.Promise[any](ctx, fmt.Sprintf("refund_changed_%d", iter)).Resolve(struct{}{})
}
```

PayoutWorkflow has **no saga compensator** — it is a pure watcher. Cancelling/releasing the payout session is part of normal Run output, not error compensation. If Run errors mid-flight, Restate will retry idempotently; payout session row remains Pending (which is correct: another invocation can resume).

---

## 10. Refund (refund-per-order)

### 10.1 Schema migration

`order.refund` is restructured from per-item to per-order:

```sql
-- additive migration (dev, no historical refunds yet)
ALTER TABLE "order".refund DROP COLUMN order_item_id;
ALTER TABLE "order".refund ADD COLUMN order_id uuid NOT NULL REFERENCES "order".order(id);
CREATE UNIQUE INDEX refund_one_active_per_order
    ON "order".refund(order_id)
    WHERE status IN ('Pending', 'Processing');
```

Regen: `make pgtempl` + `sqlc generate`.

### 10.2 Service flow (unchanged shape, parameter rename only)

`OrderHandler.CreateBuyerRefund(ctx, params)` now takes `OrderID uuid.UUID` instead of `OrderItemID int64`. Validates: order owned by buyer, order has confirmed items, no active refund for this order.

`AcceptRefundStage1`, `ApproveRefundStage2`, `RejectRefund` keep current 2-stage shape.

### 10.3 Signaling PayoutWorkflow

After **every** refund state change (create / accept / approve / reject), the refund handler fires:

```go
restate.Workflow[any](ctx, "PayoutWorkflow", orderID.String(), "OnRefundChanged").
    Send(struct{}{})
```

PayoutWorkflow loop wakes, re-reads `getRefundSnapshot`, decides next action. No payload needed — workflow is the authority on what state means.

`getRefundSnapshot(orderID)` returns:
```go
type RefundSnapshot struct {
    HasActiveRefund    bool // any refund in Pending/Processing
    LastRefundApproved bool // most recent refund row is in Success
}
```

---

## 11. Webhook handlers — biz layer indirection

### 11.1 Pattern

Webhook handlers (gateway IPN, transport status push) are HTTP endpoints with no Restate context. They parse the provider payload, then call into the Service binding:

```go
// internal/module/order/transport/webhook_vnpay.go (illustrative)
func handleVNPayIPN(c *gin.Context) {
    payload := parseAndVerify(c)
    sessionID := lookupSessionFromRefID(payload.RefID)
    restate.ServiceSend(rctx, "Order", "OnPaymentResult").Send(OnPaymentResultParams{
        SessionID: sessionID,
        TxID:      payload.TxID,
        Outcome:   payload.Outcome, // "paid" | "failed"
        ProviderData: payload.RawJSON,
    })
    c.JSON(200, gin.H{"ok": true})
}
```

### 11.2 `OnPaymentResult` (new method on `OrderHandler`)

```go
func (b *OrderHandler) OnPaymentResult(ctx restate.Context, params OnPaymentResultParams) error {
    // 1. Update DB: tx.status, tx.data, session auto-promote (existing MarkTxSuccess logic).
    _ = restate.RunVoid(ctx, updateTxStatusFn)

    // 2. Look up session.kind to decide which workflow to signal.
    session, _ := restate.Run(ctx, getSessionFn(params.SessionID))

    workflowName, workflowID := workflowForSession(session) // e.g. "CheckoutWorkflow", strconv.Itoa(session.ID)
    if workflowName == "" {
        return nil // session has no associated workflow (e.g. payout session — handled differently)
    }

    // 3. Signal the waiting workflow.
    return restate.Workflow[any](ctx, workflowName, workflowID, "ResolvePayment").
        Send(PaymentEvent{Kind: params.Outcome, TxID: params.TxID})
}
```

`workflowForSession` maps `session.kind`:
- `SessionKindBuyerCheckout` → `("CheckoutWorkflow", checkoutSessionID)`
- `SessionKindSellerConfirmationFee` → `("ConfirmWorkflow", confirmSessionID)`
- `SessionKindSellerPayout` → `("", "")` (no workflow signal needed — payout session is updated by PayoutWorkflow itself)

### 11.3 `OnTransportResult`

The existing `UpdateTransportStatus(ctx, params)` already encapsulates this pattern. It will be **renamed `OnTransportResult`** for naming symmetry, and its body extended to (optionally) signal PayoutWorkflow when transport status reaches a terminal state that affects escrow countdown (e.g., delivered confirmed → reset escrow timer to confirmed-delivery date if business rules require it; current code already fires `ReleaseEscrow` ServiceSend, which becomes a `PayoutWorkflow.OnRefundChanged`-style signal in the new world).

---

## 12. HTTP transport changes

### 12.1 `POST /buyer/checkout`

```go
func handleBuyerCheckout(c *gin.Context) {
    var input BuyerCheckoutHTTPInput
    bind(c, &input)
    sessionID := generateSessionID() // pre-allocate; passed as workflow ID
    input.SessionID = sessionID

    // Submit + attach for sync URL.
    restate.Workflow[any](rctx, "CheckoutWorkflow", strconv.FormatInt(sessionID, 10), "Run").
        Send(input)
    url, err := restate.Workflow[string](rctx, "CheckoutWorkflow", strconv.FormatInt(sessionID, 10), "WaitPaymentURL").
        Request(struct{}{})
    if err != nil { ... }

    c.JSON(200, gin.H{"checkout_session_id": sessionID, "payment_url": url})
}
```

### 12.2 `POST /seller/pending/confirm`

Symmetrical: pre-allocate `confirm_session_id`, submit `ConfirmWorkflow`, attach `WaitPaymentURL`.

### 12.3 New cancel endpoints

- `POST /buyer/checkout/:sessionID/cancel` → `restate.Workflow(...).Send` to `CancelCheckout`
- `POST /seller/pending/confirm/:sessionID/cancel` → `CancelConfirm`

---

## 13. `paymentClient.Create` RefID convention

Currently `RefID = blockerTxID`. Change to `RefID = sessionID` so webhook can directly look up workflow ID without joining tx → session.

```go
paymentClient.Create(ctx, payment.CreateParams{
    RefID:       sessionID, // was: blockerTxID
    Amount:      gatewayAmount,
    Description: fmt.Sprintf("Checkout session %d", sessionID),
})
```

`payment.CreateParams.RefID` may need to widen to handle both int64 (current) and uuid (PayoutWorkflow if applicable). Or unify on `string`. To be decided in implementation plan; not a blocker.

---

## 14. Schema changes (edit init script directly)

Repo is still in dev phase — edit `internal/module/order/db/migrations/0001_init.up.sql` directly instead of writing an additive migration file. Reset dev DB after.

Changes to `order.refund`:

- Replace column `order_item_id bigint REFERENCES "order".item(id)` with `order_id uuid NOT NULL REFERENCES "order".order(id)`.
- Add `CREATE UNIQUE INDEX refund_one_active_per_order ON "order".refund(order_id) WHERE status IN ('Pending', 'Processing');`

Optional (audit): add `workflow_id text` column to `order.payment_session` for cross-reference. Skip if not needed.

Per `feedback_no_edit_generated_queries.md`: regen via `make pgtempl` + `sqlc generate` after editing the init script. Do not hand-edit `generated_queries.sql` or `db/sqlc/*.sql.go`.

---

## 15. Migration & rollout

Dev only — no feature flag needed. Order:

1. Edit `0001_init.up.sql` for refund schema change + regen queries (`make pgtempl` + `sqlc generate`).
2. Add Saga helper.
3. Add CheckoutWorkflowHandler, register in `app/restate.go`. Update HTTP transport for `/buyer/checkout` and webhook handler.
4. Add ConfirmWorkflowHandler, register, update transport for `/seller/pending/confirm`.
5. Add PayoutWorkflowHandler, register. Update refund handlers to signal `OnRefundChanged`. Migrate existing escrow `ReleaseEscrow` ServiceSend call sites.
6. Remove legacy `BuyerCheckout` and `ConfirmSellerPending` methods from `OrderHandler`.
7. Remove `MarkTxSuccess` cross-module ServiceSend call sites in webhook handlers (replaced by `OnPaymentResult` indirection).

Each step is independently shippable.

---

## 16. Open questions deferred to implementation plan

- `payment.CreateParams.RefID` type unification (int64 vs string).
- Whether `OnTransportResult` should also be a `restate.Workflow.Send` to PayoutWorkflow when delivery completes early (vs the current `ReleaseEscrow` ServiceSend pattern).
- Whether to keep `payment_session.workflow_id` column or skip (audit value vs schema bloat).
- Whether to bundle all schema changes into one migration file or sequence them.

These do not block design approval.
