# Order Lifecycle Workflow — Design

**Date**: 2026-04-27
**Status**: Design draft, not implemented.
**Supersedes**: `internal/module/order/biz/WORKFLOW_DESIGN.md` (V1, contained design errors).

---

## 1. Goal

Replace the current Service + scattered DB-status-machine pattern for the order
lifecycle with a single Restate `Workflow` per `payment_session`. The workflow
*is* the state machine — its `Run` function is the source of truth for phase
transitions. Service handlers shrink to thin wrappers that validate input,
execute side effects, and signal the workflow.

The lifecycle has three properties that fit Workflow much better than Service:

1. **Long-lived multi-step process** — checkout → seller confirm → deliver → 7-day escrow → payout. State must persist for days.
2. **Time-driven phases** — payment expiry (30 min), escrow hold (7 d). `restate.After` is cleaner than scheduled-send + DB-status check.
3. **Race conditions between timer and external event** — escrow timer vs refund-raised. `restate.WaitFirst` handles natively; service pattern requires manual cancellation logic.

**Bug class eliminated**: implicit state machines scattered across
`payment_session.status`, `transaction.status`, `refund.status`, plus
auto-promote logic in `MarkTxSuccess`, plus timeout sweep, plus escrow timer.

---

## 2. Architecture

| Concern | Where it lives |
|---|---|
| Lifecycle state machine | `OrderWorkflow.Run` |
| External events (signals) | `OrderWorkflow` shared-context handlers |
| Synchronous DB writes / cross-module calls | Helper methods on `*OrderWorkflow`, called from `Run` |
| API endpoints (HTTP/echo) | Service handlers — validate + signal workflow |
| Read queries (List/Get) | Service handlers (unchanged) — DB queryable, journal not |

**Rule**: Workflow drives transitions. Service handlers are thin: validate input,
do idempotent atomic side-effects, signal the workflow.

`OrderWorkflow` is its **own struct**, not methods on `OrderHandler`. It holds a
reference to the handler so it can call existing biz helpers
(`CreditFromSession`, storage, account, inventory) without copying logic.

```go
type OrderWorkflow struct {
    h *OrderHandler
}

func NewOrderWorkflow(h *OrderHandler) *OrderWorkflow {
    return &OrderWorkflow{h: h}
}
```

---

## 3. Workflow ID strategy

**Workflow key = `payment_session_id` (stringified BIGINT).**

- **Why session not item**: 1 item = 1 session in current model. Session is the natural unit of "buyer's intent to pay for this thing".
- **Why session not order**: Order is created mid-lifecycle (at seller confirm). Workflow needs to start at checkout, before order exists. Workflow learns `order_id` later via `SellerDecide` signal payload.
- **Multi-item cart**: 1 cart with N items from M sellers spawns N workflows. Each runs independently. Confirm/deliver/refund signals fan out per session.

---

## 4. Phase model

```
                   ┌──────────────────────┐
                   │  Workflow created    │
                   │  (after BuyerCheckout)│
                   └──────────┬───────────┘
                              │
                ┌─────────────▼──────────────┐
PHASE 1         │  Wait for buyer to pay     │
                │  Promise: paid             │
                │  Timer: paymentExpiry      │
                └──┬──────────────────────┬──┘
                   │ paid                  │ timer
                   │                       ▼
                   │             tearDownSession(timeout)
                   │             return nil  [terminal]
                   ▼
                ┌────────────────────────────┐
PHASE 2         │  Wait for seller decision  │
                │  Promise: decision         │
                │   {Confirm, OrderID,       │
                │    PayoutSessionID}        │
                │   {Reject, Reason}         │
                └──┬──────────────────────┬──┘
                   │ confirm               │ reject
                   │                       ▼
                   │             tearDownSession(reject)
                   │             return nil  [terminal]
                   ▼
                ┌────────────────────────────┐
PHASE 3         │  Wait for delivery         │
                │  Promise: delivered        │
                └──────────┬─────────────────┘
                           │
                ┌──────────▼─────────────────┐
PHASE 4         │  Race                      │
                │  Promise: refundRaised     │
                │  Timer:   escrowWindow     │
                └──┬──────────────────────┬──┘
                   │ timer                 │ refund
                   │                       ▼
                   │              ┌─────────────────────┐
                   │              │ Wait resolution     │
                   │              │ Promise:            │
                   │              │  refundResolved     │
                   │              └──┬───────────────┬──┘
                   │                 │ approved      │ rejected
                   │                 ▼               │
                   │           return nil            │
                   │           (refund flow         │
                   │            wrote DB)            │
                   ▼◀────────────────────────────────┘
                ┌────────────────────────────┐
PHASE 5         │  releasePayout()           │
                │  return nil  [terminal]    │
                └────────────────────────────┘
```

### 4.1 Design constraints

- **1 refund per session.** If `RaiseRefund` fired and resolution = rejected, buyer cannot raise another refund. Justification: matches typical e-commerce; Restate Promise can only resolve once. To allow multi-refund would require numbered promises (`refund-attempt-N`) — added complexity not justified.
- **Refund rejected → payout immediately.** Skip remaining escrow time. Justification: refund process took days, buyer's "consideration window" is effectively closed; seller has waited long enough. Alternative (wait remaining escrow) would need promise reuse → not supported.
- **Phase 3 has no timeout.** Seller is responsible for fulfilment; if shipment never updates to Delivered, item sits indefinitely (or ops intervention). Adding a delivery timeout is a future enhancement, not in this design.

---

## 5. Code

### 5.1 Promise names + signal payloads

```go
const (
    promPaid           = "paid"
    promDecision       = "decision"
    promDelivered      = "delivered"
    promRefundRaised   = "refund-raised"
    promRefundResolved = "refund-resolved"
)

type StartOrderWorkflowParams struct {
    SessionID  int64     `json:"session_id"`
    BuyerID    uuid.UUID `json:"buyer_id"`
    ItemID     int64     `json:"item_id"`
    HasGateway bool      `json:"has_gateway"`
}

type SellerDecisionSignal struct {
    Confirm         bool      `json:"confirm"`
    OrderID         uuid.UUID `json:"order_id,omitempty"`          // set when Confirm=true
    PayoutSessionID int64     `json:"payout_session_id,omitempty"` // set when Confirm=true
    Reason          string    `json:"reason,omitempty"`            // optional, when reject
}

type RefundResolution struct {
    Approved bool   `json:"approved"`
    RefundID string `json:"refund_id"`
}
```

### 5.2 Run handler

The Run function is **forward-only**: each terminal branch calls a forward
helper directly. **No saga slice, no defer rollback.** See §6 for rationale.

```go
func (w *OrderWorkflow) Run(
    ctx restate.WorkflowContext,
    params StartOrderWorkflowParams,
) error {
    // ── PHASE 1: payment ──────────────────────────────────
    paid := restate.Promise[bool](ctx, promPaid)
    if winner := restate.WaitFirst(ctx, paid, restate.After(ctx, paymentExpiry)); winner != paid {
        return w.tearDownSession(ctx, params, "payment timeout")
    }

    // ── PHASE 2: seller decision ──────────────────────────
    decision, err := restate.Promise[SellerDecisionSignal](ctx, promDecision).Result()
    if err != nil {
        return err
    }
    if !decision.Confirm {
        return w.tearDownSession(ctx, params, "seller rejected: "+decision.Reason)
    }

    // ── PHASE 3: delivered ────────────────────────────────
    if _, err := restate.Promise[bool](ctx, promDelivered).Result(); err != nil {
        return err
    }

    // ── PHASE 4: escrow vs refund ─────────────────────────
    refundRaised := restate.Promise[bool](ctx, promRefundRaised)
    if winner := restate.WaitFirst(ctx, refundRaised, restate.After(ctx, escrowWindow)); winner == refundRaised {
        resolution, err := restate.Promise[RefundResolution](ctx, promRefundResolved).Result()
        if err != nil {
            return err
        }
        if resolution.Approved {
            return nil // refund flow already credited buyer + cancelled payout
        }
        // rejected → fall through to payout
    }

    // ── PHASE 5: payout ───────────────────────────────────
    return w.releasePayout(ctx, decision.OrderID, decision.PayoutSessionID)
}
```

### 5.3 Helpers

Two helpers, both atomic + idempotent. Each is a method on `*OrderWorkflow`.

```go
// tearDownSession — Phase 1 timeout & Phase 2 reject.
//
// Handles BOTH session statuses:
//   - Pending  (Phase 1 timeout, no payment yet)        → Failed
//   - Success  (Phase 2 reject after payment confirmed) → Cancelled
//
// Uses MarkPaymentSessionTerminated, a single CASE-WHEN query that flips both
// in one statement (see §10).
//
// Idempotent: every DB statement filters on a status guard, so re-running on
// Restate replay is a no-op. Cross-module calls (CreditFromSession,
// ReleaseInventory) rely on Restate's exactly-once journal — they are invoked
// at most once per workflow because workflow_id == session_id is unique and
// teardown is a terminal branch (see §10.2).
func (w *OrderWorkflow) tearDownSession(
    ctx restate.Context,
    p StartOrderWorkflowParams,
    reason string,
) error {
    item, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderItem, error) {
        if _, e := w.h.storage.Querier().MarkPaymentSessionTerminated(rctx, p.SessionID); e != nil && !errors.Is(e, pgx.ErrNoRows) {
            return orderdb.OrderItem{}, e
        }
        it, e := w.h.storage.Querier().CancelItem(rctx, orderdb.CancelItemParams{
            ID:            p.ItemID,
            CancelledByID: uuid.NullUUID{UUID: p.BuyerID, Valid: true},
        })
        if e != nil && !errors.Is(e, pgx.ErrNoRows) {
            return orderdb.OrderItem{}, e
        }
        return it, nil
    }, restate.WithName("tearDown.dbWrites"))
    if err != nil {
        return err
    }

    if _, err = w.h.CreditFromSession(ctx, CreditFromSessionParams{
        SessionID:  p.SessionID,
        AccountID:  p.BuyerID,
        CreditType: "Refund",
        Reference:  fmt.Sprintf("wf-teardown:%d", p.SessionID),
        Note:       reason,
    }); err != nil {
        return err
    }

    return w.h.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
        Items: []inventorybiz.ReleaseInventoryItem{{
            RefType: inventorydb.InventoryStockRefTypeProductSku,
            RefID:   item.SkuID,
            Amount:  item.Quantity,
        }},
    })
}

// releasePayout — Phase 5 happy path.
// Marks the pending payout-session tx Success (auto-promotes the session via
// MarkTxSuccess) and credits the seller's wallet.
func (w *OrderWorkflow) releasePayout(
    ctx restate.Context,
    orderID uuid.UUID,
    payoutSessionID int64,
) error {
    pendingTxID, err := restate.Run(ctx, func(rctx restate.RunContext) (int64, error) {
        txs, e := w.h.storage.Querier().ListTransactionsBySession(rctx, payoutSessionID)
        if e != nil {
            return 0, e
        }
        for _, tx := range txs {
            if tx.Status == orderdb.OrderStatusPending && tx.Amount > 0 && !tx.ReversesID.Valid {
                return tx.ID, nil
            }
        }
        return 0, ordermodel.ErrTxNotFound.Terminal()
    })
    if err != nil {
        return err
    }

    if err := w.h.MarkTxSuccess(ctx, MarkTxSuccessParams{TxID: pendingTxID, DateAt: time.Now()}); err != nil {
        return err
    }

    order, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderOrder, error) {
        return w.h.storage.Querier().GetOrder(rctx, uuid.NullUUID{UUID: orderID, Valid: true})
    })
    if err != nil {
        return err
    }

    _, err = w.h.CreditFromSession(ctx, CreditFromSessionParams{
        SessionID:  payoutSessionID,
        AccountID:  order.SellerID,
        CreditType: "Payout",
        Reference:  fmt.Sprintf("wf-payout:%d", payoutSessionID),
        Note:       "escrow release",
    })
    return err
}
```

### 5.4 Signal handlers

Each signal handler is a thin wrapper that resolves one named Promise.

```go
func (w *OrderWorkflow) ConfirmPayment(ctx restate.WorkflowSharedContext) error {
    return restate.Promise[bool](ctx, promPaid).Resolve(true)
}

func (w *OrderWorkflow) SellerDecide(
    ctx restate.WorkflowSharedContext,
    d SellerDecisionSignal,
) error {
    return restate.Promise[SellerDecisionSignal](ctx, promDecision).Resolve(d)
}

func (w *OrderWorkflow) MarkDelivered(ctx restate.WorkflowSharedContext) error {
    return restate.Promise[bool](ctx, promDelivered).Resolve(true)
}

func (w *OrderWorkflow) RaiseRefund(ctx restate.WorkflowSharedContext) error {
    return restate.Promise[bool](ctx, promRefundRaised).Resolve(true)
}

func (w *OrderWorkflow) ResolveRefund(
    ctx restate.WorkflowSharedContext,
    r RefundResolution,
) error {
    return restate.Promise[RefundResolution](ctx, promRefundResolved).Resolve(r)
}
```

**Open question (Q1, §7)**: Whether `restate.WorkflowReflect` registers handlers
that take only `WorkflowSharedContext` (no payload). If not, the no-payload
signals (`ConfirmPayment`, `MarkDelivered`, `RaiseRefund`) need a `restate.Void`
or `Empty struct{}` second parameter.

### 5.5 Registration

```go
endpoint := server.NewRestateServer(...).
    Bind(restate.Reflect(handler)).                               // OrderHandler service
    Bind(restate.WorkflowReflect(NewOrderWorkflow(handler)))      // OrderWorkflow

// Configure retention to cover full lifecycle (escrow window + buffer).
// Open question (Q3, §7): exact knob name in Go SDK.
```

---

## 6. Why no saga (rationale)

The original V1 design (`internal/module/order/biz/WORKFLOW_DESIGN.md`) used a
saga slice with `slices.Backward` rollback. That was wrong for this workflow.

**Saga fits when**: a workflow does N sequential mutating acts, and a failure of
step K must roll back steps 1..K-1. Canonical example (Restate trip booking):

```js
const compensations = [];
const flightBooking = await flights.reserve(tripID);
compensations.push(() => flights.cancel(tripID, flightBooking));

const carBooking = await carRentals.reserve(tripID);
compensations.push(() => carRentals.cancel(tripID, carBooking));

const payment = await payments.process(tripID);
compensations.push(() => payments.refund(tripID, payment));
```

Each `act` is in the workflow. Each `act` has a meaningful undo. A failure
rolls back the chain.

**Order workflow does not have this shape**:

| Phase | Does workflow do an in-Run mutating act? |
|---|---|
| 1 Wait paid | No — observes signal |
| 2 Wait seller decision | No — observes signal |
| 3 Wait delivered | No — observes signal |
| 4 Wait escrow/refund | No — observes signal/timer |
| 5 Payout | **Yes** — but terminal; no later step can fail |

Workflow has **zero in-Run pre-terminal acts**. A saga slice would be empty for
the entire Run. Adding it as scaffolding for "future expansion" is cargo-cult.

**What about Phase 1 timeout / Phase 2 reject cleanup?** Those are forward
terminal writes (mark session Failed, cancel item, refund settled portion,
release inventory) — not undos. State moves *forward* to a terminal Failed
state. A forward helper (`tearDownSession`) is the right tool. Naming it a
"saga compensation" would be metaphor abuse.

**BuyerCheckout already has its own saga** for *its* acts (reserve inventory,
create session, debit wallet). Workflow doesn't need a second saga for the same
acts.

**When to revisit**: if a future change introduces an in-Run mutating act
between Phase 1 and Phase 5 (e.g. workflow itself creates the order in Phase 2),
add a saga slice **at that point** — act-then-append.

```go
// Hypothetical future: Phase 2 act with comp.
order, err := w.createOrder(ctx, params, decision)  // act
if err != nil { return err }
compensations = append(compensations, func() error { return w.deleteOrder(ctx, order.ID) })
```

Rule: **add saga when there's a chain to compensate, not before.**

---

## 7. Open questions / Verification status

| # | Question | Status | Mitigation / Action |
|---|---|---|---|
| Q1 | Can `restate.WorkflowReflect` bind handlers with only `WorkflowSharedContext` (no payload)? | ⚠️ Verify | If not, add `_ restate.Void` or `_ Empty` second param to no-payload signals. |
| Q2 | `restate.WaitFirst` semantics in Go SDK v0.24.0 — winner identity-comparable to original future? | ✅ Confirmed (V1 §9 verification) | Use `if winner != paid` / `if winner == refundRaised`. |
| Q3 | Workflow retention configuration knob name + default in Go SDK | ⚠️ Verify | When registering, set retention ≥ paymentExpiry + escrowWindow + buffer (≈ 14 d). Check Restate server ≥ 1.4. |
| Q4 | Promise.Resolve double-call behavior | ⚠️ Verify | Add idempotency guard in signal handlers if Resolve errors on second call. (Likely errors per docs.) |
| Q5 | Signal arrival before workflow exists — does Restate buffer? | ⚠️ Verify | Integration test: signal first, submit Run, observe Run wakes immediately. |
| Q6 | Code-change replay semantics (deterministic replay constraint on Run body) | ⚠️ Operational | Treat Run body as stable contract: only append new steps after existing journaled steps; for major changes, version workflow name (`OrderWorkflowV2`) or drain in-flight workflows. In dev, clear Restate state freely. |

---

## 8. Service handler integration

Signal calls go to `*OrderWorkflow` methods. Service handlers obtain the
workflow client via the typed pattern (Go SDK `restate.WorkflowSend(ctx, OrderWorkflow.X, sessionID)`).

| Current handler | After workflow refactor |
|---|---|
| `BuyerCheckout` | Same body. End: for each session created, `restate.WorkflowSend(ctx, OrderWorkflow.Run, sessionID).Send(StartOrderWorkflowParams{...})`. For pure-wallet sessions, also fire `ConfirmPayment` after `MarkTxSuccess` settles the wallet leg. |
| `MarkTxSuccess` (webhook) | After flipping tx Success + auto-promoting session: if session Status now Success and kind = `buyer-checkout`, fire `ConfirmPayment(sessionID)`. |
| `MarkTxFailed` (webhook) | Mark tx Failed. Workflow's Phase 1 timer eventually fires teardown. (Optional: add `FailPayment` signal for immediate response.) |
| `CancelBuyerPending` | **Removed.** Buyer-cancel pre-confirm becomes a new `BuyerCancel` signal handler that resolves a `cancelled` promise. Phase 1 changes to a 3-way race: `WaitFirst(paid, cancelled, paymentExpiry)`. Any winner ≠ `paid` calls `tearDownSession`. (Alternative: omit the signal and let the buyer wait for natural timeout — worse UX, not chosen.) |
| `ConfirmSellerPending` | Same body through creating the order + linking items. End: per item, `SellerDecide(item.PaymentSessionID, {Confirm:true, OrderID, PayoutSessionID})`. |
| `RejectSellerPending` | Validates only. Per item: `SellerDecide(item.PaymentSessionID, {Confirm:false, Reason})`. No DB mutations — workflow's `tearDownSession` handles cleanup. |
| `UpdateTransportStatus` (webhook) | On Delivered: load all items in the transport's order, fan out `MarkDelivered(item.PaymentSessionID)` per item. |
| `CreateBuyerRefund` | Same body — creates refund row + return transport. End: `RaiseRefund(item.PaymentSessionID)`. |
| `AcceptRefundStage1` | Unchanged — internal stage of the refund flow, no signal needed. |
| `ApproveRefundStage2` | Same body — inserts negative tx + cancels item + cancels payout session. End: `ResolveRefund(item.PaymentSessionID, {Approved:true, RefundID})`. |
| `RejectRefund` | Same body — refund.status = Failed. End: `ResolveRefund(item.PaymentSessionID, {Approved:false, RefundID})`. |
| `ReleaseEscrow` | **Removed** as a separately scheduled handler. Becomes `OrderWorkflow.releasePayout`, called from Run Phase 5. |
| `TimeoutCheckoutSession` | **Removed.** Phase 1 `restate.After(paymentExpiry)` replaces it. |
| `TimeoutConfirmFeeSession` | **Removed** if confirm-fee is absorbed as a sub-step; otherwise own workflow. (Decide separately, not in this design.) |

**Net result**: 5–6 service files (`tx_mark`, `escrow`, `timeout`, parts of
`checkout`/`confirm`/`reject`/`refund`) shrink or get deleted. New file
`order_workflow.go` (~250 lines) holds the lifecycle logic.

---

## 9. Multi-item fan-out

When 1 cart has N items confirmed into 1 order:

```go
// Inside ConfirmSellerPending, after CreateDefaultOrder + SetItemsOrderID:
for _, item := range orderItems {
    restate.WorkflowSend(ctx, OrderWorkflow.SellerDecide,
        strconv.FormatInt(item.PaymentSessionID, 10)).
        Send(SellerDecisionSignal{
            Confirm:         true,
            OrderID:         order.ID,
            PayoutSessionID: payoutSession.ID,
        })
}
```

Same pattern for delivery / refund signals — load items by order ID, send
signal per session ID.

**Cost**: N RPC sends per fan-out event. For typical N (1–10 items per order),
negligible. Restate batches efficiently.

---

## 10. Schema impact

**Largely unchanged.** Status fields stay because read queries need them
(dashboards, admin, list views). They are denormalized projections of workflow
state — written by helpers called from workflow `Run`, never directly by
callers.

### 10.1 New query required

`tearDownSession` (§5.3) needs to flip session status for BOTH cases:

- Phase 1 timeout: session is `Pending` → `Failed`
- Phase 2 reject after Phase 1 paid: session is `Success` → `Cancelled`

The existing `MarkPaymentSessionFailed` only handles `Pending`. Add a new query
that handles both in one CASE-WHEN statement:

```sql
-- name: MarkPaymentSessionTerminated :one
-- Used by OrderWorkflow.tearDownSession (Phase 1 timeout & Phase 2 reject).
-- Flips Pending → Failed (timeout case) or Success → Cancelled (post-pay
-- reject case). No-op if already in a terminal state. Idempotent.
UPDATE order.payment_session
SET status = CASE
        WHEN status = 'Pending' THEN 'Failed'
        WHEN status = 'Success' THEN 'Cancelled'
        ELSE status
    END,
    date_modified = NOW()
WHERE id = $1
  AND status IN ('Pending', 'Success')
RETURNING *;
```

Add to `internal/module/order/db/queries/payment_session.sql`. Run pgtempl +
sqlc generate.

`MarkPaymentSessionFailed` and `MarkPaymentSessionCancelled` stay (used by other
flows: refund payout cancellation, gateway timeout edge cases).

### 10.2 Cross-module idempotency model

Verified against current code (`internal/module/account/biz/wallet.go`,
`internal/module/inventory/biz/release.go`):

| Module | Current behavior | Idempotency source |
|---|---|---|
| `account.WalletCredit` | UPDATE `profile.balance += amount`. **`Reference` field exists in params but is unused** (no dedup index). | **Restate journal exactly-once.** Each cross-module call from workflow is journaled once; replay returns cached result without re-execution. |
| `inventory.ReleaseInventory` | UPDATE inventory_stock; returns `ErrInsufficientReservedInventory` if `rows == 0`. **Not natively idempotent** (re-release would error). | **Restate journal exactly-once + workflow uniqueness** (only one workflow per session_id; teardown path is a single terminal branch, called at most once). |

**Why this is safe**: Restate guarantees a `restate.Run` block (or RPC) executes
exactly once per workflow invocation; replay reads the journal. Workflow ID =
session_id is unique. Each session has at most one teardown call (Phase 1
timeout XOR Phase 2 reject). So `WalletCredit` and `ReleaseInventory` are each
invoked at most once per session, regardless of replay.

**Risk**: if a future change adds a second invocation path (e.g. external admin
"force release inventory" tool), idempotency breaks. To future-proof:

- **Optional**: wire `WalletCredit` to honor `Reference` — add
  `account.wallet_ledger` table with `UNIQUE(account_id, reference)`, insert
  before balance UPDATE. Spec marks Reference as authoritative dedup key.
- **Optional**: make `ReleaseInventory` no-op on `rows == 0` instead of
  erroring. Cleaner contract.

These are **not blockers** for this design — current behavior is sufficient.
Track as follow-ups.

### 10.3 Could remove (eventually, after soak)

- `payment_session.date_expired` — workflow timer is source of truth. Keep if
  read APIs display it.
- Auto-promote logic in `MarkTxSuccess` — workflow knows when to promote (if
  later refactor moves promote into workflow act).

### 10.4 Definitely keep

- All `*.status` fields — read APIs query them.
- `transaction.reverse_id` — refund traceability.
- `payment_session.reverses_session_id` (self-FK) — refund-payout linking.

---

## 11. Rollout

Phases (greenfield-leaning, since dev is in flux):

1. **PR 1: Add OrderWorkflow code** — new file `order_workflow.go` with `Run` + helpers + signal handlers. Compile clean. Register in Restate binding. Run integration tests for Q1, Q4, Q5.
2. **PR 2: Wire BuyerCheckout to submit workflow** — keep existing timeout/escrow handlers as fallback (defensive). Workflow runs in parallel, no harm.
3. **PR 3: Migrate signals one phase at a time** —
   - 3a: `ConfirmPayment` from `MarkTxSuccess`.
   - 3b: `SellerDecide` from `Confirm/RejectSellerPending`.
   - 3c: `MarkDelivered` from transport webhook.
   - 3d: `RaiseRefund` + `ResolveRefund` from refund flow.
4. **PR 4: Disable old timeout/escrow handlers** — workflow now drives 100%. Remove `TimeoutCheckoutSession`, `TimeoutConfirmFeeSession`, scheduled `ReleaseEscrow` send.
5. **PR 5: Cleanup** — remove now-unused saga compensation in `BuyerCheckout` (workflow's Phase 1 timeout covers it) if applicable.

Each PR independently revertable, build-green, no big-bang.

---

## 12. Decision criteria

Go ahead if:
- Restate server ≥ 1.4 (workflow retention configurable). Verified: `deployment/docker-compose.yml` pulls latest.
- Schema-refactor branch (`payment_session` + `transaction` split) lands and soaks for 2–3 days first — don't stack two major refactors mid-flight.
- Q1 + Q3 + Q4 verified at PR 1 (integration test).

Hold off if:
- Restate server < 1.4 (workflow retention not configurable — bad for 7 d hold).
- Schema refactor reveals additional issues that need redesign.
- Q1 (no-payload signal binding) blocks; if so, add `Empty` param uniformly.

---

## 13. Glossary

- **`restate.WorkflowContext`**: full workflow context, used inside `Run`. Implements `restate.Context`.
- **`restate.WorkflowSharedContext`**: read-only context for signal handlers. Cannot drive `Run` logic.
- **`restate.Run`**: durable side-effect block. Each call produces one journal entry. Result is JSON-serialized.
- **`restate.Promise[T]`**: named, single-resolve future scoped to a workflow ID. Survives workflow replay.
- **`restate.WaitFirst`**: race primitive. Returns the winning future. Replaces deprecated `restate.Select`.
- **`restate.After`**: durable timer future, can be combined with promises in `WaitFirst`.
- **Forward terminal helper**: a method that writes the system to a terminal failed state (`tearDownSession`). Idempotent. Not a saga compensation.
- **Saga compensation**: rollback function for an in-Run mutating act, registered with act-then-append. Not used in this design.
