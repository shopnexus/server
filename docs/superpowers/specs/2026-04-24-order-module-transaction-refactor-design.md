# Order Module — Transaction Ledger Refactor

**Date:** 2026-04-24
**Scope:** `internal/module/order/` only (biz + transport + model + sqlc queries). Cross-module boundaries (account, catalog, common, etc.) untouched — stubs or existing signatures assumed.
**Approach:** Full slice (Approach 3 from brainstorming). Biz rewritten, interface free to change, transport/echo updated, tests deleted (not rewritten), migration performed by user.

## Context

The `order` database schema was restructured. The previous per-checkout `payment` table was replaced with a universal `transaction` ledger. Transport lost its `cost` column, order lost its financial aggregation columns, refund became a 2-stage workflow, dispute comments were genericized across roles.

The biz layer still references the old schema everywhere. This spec defines the rewrite.

## Non-goals

- Refactoring account, catalog, chat, common, promotion, analytic, inventory modules.
- Rewriting deleted tests.
- Introducing new metrics, new observability, or new API versions.
- Optimizing Restate `Run()` closure JSON serialization — full structs are returned (cleanup later if round-trip bugs surface).

## 1. Transaction Ledger Semantics

### 1.1 Type values

Convention: single snake_case event name. Direction is carried by `from_id`/`to_id` columns; type names describe the event only.

| Constant | String value | From → To | Trigger |
|---|---|---|---|
| `TxTypeCheckout` | `checkout` | buyer → platform (NULL) | Buyer checkout |
| `TxTypeConfirmFee` | `confirm_fee` | seller → platform (NULL) | Seller confirms items |
| `TxTypePayout` | `payout` | platform (NULL) → seller | Escrow release |
| `TxTypeRefund` | `refund` | platform (NULL) → buyer | Refund approved / cancel |

Platform has no account row → `from_id=NULL` / `to_id=NULL` for platform side. `amount` is always positive; direction is inferred from type.

### 1.2 Status transitions per type

Uses `order.status` enum: `Pending | Processing | Success | Cancelled | Failed`.

| Type | Creation status | Success path | Cancelled / Failed path |
|---|---|---|---|
| `checkout` wallet | Success (ngay) | — | — |
| `checkout` gateway | Pending | Webhook confirms → Success + `date_paid` set | Timeout past `date_expired` → Failed; cancel item, release inventory |
| `confirm_fee` wallet | Success (ngay) | — | — |
| `confirm_fee` gateway | Pending | Webhook confirms → Success | Timeout → Failed; delete order row, unlink items (items return to "paid" state), seller may retry confirm |
| `payout` | Always Pending | Escrow release (see §2) → Success + WalletCredit seller | Refund approved before release → Cancelled (no money moves) |
| `refund` | Always Success (WalletCredit buyer at creation) | — | — |

Note: `payout` uses `Cancelled`, not `Failed`, for the "refund-wins" race because Failed implies external error; Cancelled implies internal policy decision.

### 1.3 Currency fields

- `amount` positive; `from_currency`, `to_currency`, `exchange_rate` snapshotted at tx creation.
- Wallet-only tx: `from_currency == to_currency == profile.country-derived currency`.
- Gateway tx: `from_currency = buyer currency`, `to_currency = seller/platform currency`; exchange rate snapshot at creation.

### 1.4 Hybrid (wallet + gateway) rule

When a payer (buyer at checkout, seller at confirm) ticks "use wallet" but wallet balance is insufficient:
1. Create wallet tx (Success) for the portion covered by balance.
2. Create gateway tx (Pending) for the remaining amount.
3. The FK pointer (`item.payment_tx_id` / `order.seller_tx_id`) points to the **gateway tx** (the blocker). This is application convention, not a DB constraint.

Pure wallet path: FK points to wallet tx (Success). Pure gateway path: FK points to gateway tx (Pending).

### 1.5 Invariants

- `item.payment_tx_id.status = Success` ⟺ item is "effectively paid".
- `order` row exists ⟺ `order.seller_tx_id` exists (NOT NULL).
- `item.refund_tx_id IS NOT NULL` ⟹ `item.date_cancelled IS NOT NULL`.
- `payout.status = Success` ⟹ `transport.status = Delivered` AND no active refund on any item AND (7-day timer expired).

## 2. Order Lifecycle State Machine

No `order.status` column. State is **projected** from tx statuses, `item.date_cancelled`, and `transport.status`. Source of truth is the ledger.

### 2.1 Happy path

```
[Cart] → user checkout
  ↓
[Items created + checkout tx(s)]    // item.payment_tx_id NOT NULL from creation
  ↓
[checkout tx Success]               // wallet instant OR gateway webhook
  ↓
[Seller confirm] → transport created, confirm_fee tx(s), payout tx (Pending), order created
  ↓
[confirm_fee tx Success]            // wallet instant OR gateway webhook
  ↓
[transport.status → Delivered]      // provider webhook
  ↓
[7-day escrow timer fires]          // Restate timer, no "received" button
  ↓
[payout tx → Success, WalletCredit seller]
  ↓
[Settled]
```

### 2.2 Unhappy transitions

| From state | Trigger | Result |
|---|---|---|
| checkout gateway Pending | `tx.date_expired` reached | checkout tx → Failed; `item.date_cancelled=now`, `cancelled_by_id=NULL`; release inventory |
| Item paid (order_id NULL) | Buyer cancel | Create `refund` tx (Success, WalletCredit buyer amount=`paid_amount`); `item.date_cancelled=now`, `cancelled_by_id=buyer`, `item.refund_tx_id=new tx`; release inventory |
| Item paid (order_id NULL) | Seller reject | Same as buyer cancel; `cancelled_by_id=seller` |
| Item paid (order_id NULL) | No seller confirm within N days | Auto-cancel as system-initiated reject (`cancelled_by_id=NULL`) |
| Order created, confirm_fee Pending | Timeout | confirm_fee tx → Failed; payout tx → Failed; delete order row; unlink items (set `order_id=NULL`); items return to "paid" state; seller can retry confirm |
| Order confirmed, any transport state | Buyer refund request | Enter 2-stage flow (§3); does not immediately cancel anything |
| transport.Delivered within 7-day escrow | Buyer refund request | Same 2-stage flow; prevents auto-payout |
| transport.Delivered, 7d expired, no active refund | Restate escrow timer fires | payout → Success + WalletCredit seller |
| transport.Delivered, 7d expired, active refund exists | Restate escrow timer fires | Skip release; refund flow will later set payout → Cancelled on approve, or fire another release on reject |

### 2.3 Note on refund vs pre-confirm cancel

The `refund` table (and its 2-stage workflow in §3) is only used for **post-confirm** refund requests (item already has `order_id`). A **pre-confirm** cancel (buyer or seller or system) creates a `refund` tx directly on the item (`item.refund_tx_id`) without a `refund` table row — there is no ongoing request to manage, it is immediate. This distinction is reflected in the tables above.

### 2.4 Invariants

- `item.order_id IS NULL` ⟺ item is unconfirmed OR was cancelled before confirm.
- `item.date_cancelled IS NOT NULL` is terminal — no "uncancel".
- Order row exists ⟹ at least one item has `order_id = this order`.
- Exactly one `payout` tx per order; `order.seller_tx_id` references the `confirm_fee` tx (or blocker if hybrid), not the payout tx.

## 3. Refund 2-Stage Workflow

### 3.1 State machine

```
Buyer requests refund on an item (item.paid, not cancelled, no active refund)
  ↓
[refund.Pending]  (accepted_by_id, approved_by_id, refund_tx_id all NULL)
  │
  ├── Seller rejects stage 1 → refund.Failed, rejection_note set; buyer may raise dispute
  │
  └── Seller accepts stage 1
        ↓
      [refund.Processing]  (accepted_by_id + date_accepted set; return transport created)
        ↓
      Buyer physically returns item; return transport.status updates
        ↓
      transport.status = Delivered (seller received)
        ↓
        ├── Seller rejects stage 2 → refund.Failed, rejection_note set; seller may raise dispute (hàng hỏng do buyer)
        │
        └── Seller approves stage 2
              ↓
            Create refund tx (type=refund, from=NULL, to=buyer, Success, WalletCredit buyer = item.paid_amount)
            Set refund.refund_tx_id, refund.approved_by_id, refund.date_approved
            Set item.date_cancelled=now, item.cancelled_by_id=buyer, item.refund_tx_id=new tx
            Set payout.status = Cancelled (if still Pending)
              ↓
            [refund.Success]
```

### 3.2 Invariants

- `refund.Pending` ⟹ `accepted_by_id IS NULL AND approved_by_id IS NULL AND refund_tx_id IS NULL`.
- `refund.Processing` ⟹ `accepted_by_id IS NOT NULL AND approved_by_id IS NULL AND refund_tx_id IS NULL`.
- `refund.Success` ⟹ `accepted_by_id IS NOT NULL AND approved_by_id IS NOT NULL AND refund_tx_id IS NOT NULL`.
- `refund.Failed` ⟹ `rejection_note IS NOT NULL` AND (stage 1: `accepted_by_id IS NULL`; stage 2: `accepted_by_id IS NOT NULL AND approved_by_id IS NULL`).
- At most one `Pending` or `Processing` refund per `order_item_id`. After `Failed`, buyer may request again (new refund row).

### 3.3 Payout–refund race handling

Guard at query time, not via schema flag:

- When the 7-day escrow timer fires for a given order, query: "any refund on any item of this order where `status IN (Pending, Processing)`?" If yes, skip release (the refund flow is responsible for final resolution).
- When refund stage 2 approves, unconditionally set related `payout.status = Cancelled` (if still Pending).
- If a refund is rejected (Failed) after escrow expired, the timer already fired; we re-fire release by scheduling a new short Restate timer (e.g., 1 minute) on reject-stage-1/2 → verifies no *other* refunds block → releases payout if clear.

### 3.4 Dispute

Either buyer or seller can raise a dispute against a refund Failed decision (schema already fixed). `refund_dispute.note` is NOT NULL — require resolution note at resolution time.

Dispute approve → force-create refund tx, set `refund.refund_tx_id`, set `refund.status=Success`.
Dispute reject → leave refund as Failed, dispute Failed, no money moves.

## 4. Biz Layer File-by-File Changes

### 4.1 Scope table

| File | Change |
|---|---|
| `interface.go` | Rewrite method signatures (see §4.2). |
| `cart.go` | Minimal; cart_item schema unchanged. |
| `checkout.go` | Major rewrite. Replace payment session creation with tx creation (wallet Success + optional gateway Pending). Enforce `item.payment_tx_id NOT NULL`. |
| `incoming.go` | Major rewrite. Create confirm_fee tx(s) + payout tx (Pending) + transport + order (eager creation with rollback-on-timeout). |
| `order.go` | Medium. Drop `product_cost/discount/total` references. Total derived as `SUM(item.paid_amount)`. |
| `refund.go` | Major rewrite. 1-stage → 2-stage: split into `AcceptRefundStage1`, `ApproveRefundStage2`, `RejectRefund`. Refund tx created only at stage 2 approve. |
| `dispute.go` | Rename `issued_by_id` → `account_id`. Either-party permission check (`account_id` must be buyer of item OR seller of item). |
| `transport_update.go` | Major rewrite. Use `OrderStatus` enum (transport_status enum gone). On Delivered → schedule 7-day Restate escrow timer. |
| `timeout.go` | Expanded: handles checkout-tx timeout, confirm_fee-tx timeout, and escrow timer fire. |
| `payment_map.go` | Likely delete (was payment→model mapping). Replace with `transaction_map.go` if domain mapping is needed. |
| `transport_map.go` | Minor; remove `cost` references. |
| `dashboard.go` | Medium. Seller dashboard queries use `SUM(item.paid_amount)` via SQL aggregation. |
| `export_test.go` | Delete if unused after test deletion. |
| `refund_test.go`, `transport_update_test.go` | Delete. |
| `restate_gen.go` | Regenerated by `go generate`. |

### 4.2 New method signatures (sketch)

```go
// interface.go — key changes

type CheckoutParams struct {
    Account         accountmodel.Account
    ItemIDs         []int64         // cart_item IDs to convert to order.item
    UseWallet       bool            // tick box
    PaymentOption   string          // common.service_option.id for gateway portion
    InstrumentID    *uuid.UUID      // account.wallet.id for gateway portion
    Address         string          // snapshot at checkout
    TransportOption string
    Note            string
}

type CheckoutResult struct {
    ItemIDs                []int64
    CheckoutTxIDs          []int64
    BlockerTxID            int64   // item.payment_tx_id points here
    RequiresGatewayPayment bool    // true if blocker status is Pending
    GatewayURL             *string // provider redirect URL if gateway
}

type ConfirmSellerPendingParams struct {
    Account         accountmodel.Account
    ItemIDs         []int64
    UseWallet       bool
    PaymentOption   string
    InstrumentID    *uuid.UUID
    Note            string
}

type ConfirmSellerPendingResult struct {
    OrderID                uuid.UUID
    ConfirmFeeTxIDs        []int64
    PayoutTxID             int64
    BlockerTxID            int64
    RequiresGatewayPayment bool
    GatewayURL             *string
}

type AcceptRefundStage1Params struct {
    Account               accountmodel.Account   // seller
    RefundID              uuid.UUID
    ReturnTransportOption string
}

type ApproveRefundStage2Params struct {
    Account  accountmodel.Account   // seller
    RefundID uuid.UUID
}

type RejectRefundParams struct {
    Account        accountmodel.Account   // seller
    RefundID       uuid.UUID
    Stage          int                    // 1 or 2
    RejectionNote  string                 // required
}

type MarkTxSuccessParams struct {
    TxID    int64
    DateAt  time.Time   // from webhook (provider timestamp); falls back to now
}

type MarkTxFailedParams struct {
    TxID    int64
    Reason  string
}
```

### 4.3 Restate discipline

- Full structs **are allowed** as `restate.Run()` return values for readability. If JSON round-trip bugs surface (uuid.UUID byte arrays, null.* unmarshal issues, int64 precision), switch that specific closure to minimal struct.
- Cross-module calls (`b.account.WalletDebit`, `b.inventory.Release`, etc.) must stay **outside** `restate.Run()` blocks.
- Biz methods must accept `restate.Context`; init-time callers (escrow timer registration, startup hooks) use `context.Context` and call through the Restate HTTP ingress client.

### 4.4 Escrow timer implementation

Use Restate timer pattern (existing `timeout.go` has the shape). On `transport.status → Delivered`:

```go
// pseudocode inside UpdateTransportStatus biz method
if newStatus == orderdb.OrderStatusSuccess /* Delivered mapped to Success */ {
    restate.ServiceSend(ctx, serviceName, "ReleaseEscrow").
        WithDelay(7 * 24 * time.Hour).
        Send(ReleaseEscrowParams{OrderID: orderID})
}
```

`ReleaseEscrow` handler queries active refunds on any item of the order; if none → sets payout Success + WalletCredit seller; if any → returns silently (refund flow will re-schedule or set Cancelled as appropriate).

## 5. Transport (echo) Layer Changes

### 5.1 Route changes

| File | Routes affected |
|---|---|
| `echo/refund.go` | Replace single `PATCH /refunds/:id` confirm with 3 endpoints: `POST /refunds/:id/accept` (stage 1), `POST /refunds/:id/approve` (stage 2), `POST /refunds/:id/reject` (body: `{stage, rejection_note}`). |
| `echo/incoming.go` | `POST /confirm` body schema adds `use_wallet`, `payment_option`, `instrument_id`. Response includes `gateway_url` when gateway path is used. |
| `echo/order.go` | Order response schema drops financial aggregation columns; adds computed `total_amount`, plus arrays of related tx summaries (optional). |
| `echo/dispute.go` | Permission check expanded: either buyer (if account owns the refunded item) or seller (if account is the seller of the refunded item) may create a dispute. |
| `echo/cart.go` | Unchanged. |

### 5.2 Webhook callback adapters

- `onResult` (payment): swap `biz.ConfirmPayment(refID, status)` for `biz.MarkTxSuccess(txID)` or `biz.MarkTxFailed(txID)`. Gateway must be passed `tx.id` at creation (stored in `tx.data` JSON as `{"gateway_ref": "..."}` or as the `ref_id` sent to the provider).
- `onTransportResult`: swap `orderdb.OrderTransportStatus` → `orderdb.OrderStatus` in the mapping; logic is otherwise unchanged.

## 6. Model Package Changes

| File | Change |
|---|---|
| `model/order.go` | Rewrite `Order` domain struct: drop `ProductCost/Discount/TransportCost/Total/Note/Data`; add `TotalAmount int64` (computed). Add new `Transaction` struct (domain mirror of `OrderTransaction`). Add `Order.CheckoutTxs []Transaction`, `Order.ConfirmFeeTx *Transaction`, `Order.PayoutTx *Transaction`. |
| `model/order.go` | Rewrite `Item`: `UnitPrice → SubtotalAmount`; drop `TransportCostEstimate/DateUpdated`; add `CancelledByID *uuid.UUID`, `RefundTxID *int64`, `PaymentTxID int64`. |
| `model/order.go` | Rewrite `Refund`: drop `ItemIDs/Amount`; add `OrderItemID int64`, `AcceptedByID/DateAccepted`, `ApprovedByID/DateApproved`, `RefundTxID *int64`, `RejectionNote *string`. |
| `model/error.go` | Add: `ErrTxNotFound`, `ErrTxAlreadyFinal`, `ErrInsufficientWalletBalance`, `ErrRefundStageSkipped` (stage 2 without stage 1 accepted), `ErrOrderHasActiveRefund` (payout guard), `ErrRefundRejectionWithoutReason`. Keep existing. |
| `model/notify.go` | New notification event constants aligned with tx events. |
| `model/cart.go` | No change. |

## 7. Implementation Order

1. Delete orphan `internal/module/account/db/sqlc/payment_method_custom.sql.go` so `go build ./internal/module/order/...` compiles its dependency. Stub `WalletDebit`/`WalletCredit` in `account/biz/wallet.go` if they're broken (minimum edit to preserve signatures).
2. Update `order/db/queries/*.sql` custom queries for new schema. Regenerate:
   ```bash
   go run ./cmd/pgtempl/ -module order -skip-schema-prefix -single-file=generated_queries.sql
   sqlc generate
   ```
3. Rewrite `order/model/*`.
4. Rewrite `order/biz/interface.go`.
5. Rewrite biz files in dependency order: `payment_map.go` (or delete) → `checkout.go` → `incoming.go` → `transport_update.go` → `refund.go` → `dispute.go` → `timeout.go` → `order.go` → `dashboard.go` → `cart.go`.
6. Regenerate Restate proxies: `go generate ./internal/module/order/biz/`.
7. Update `order/transport/echo/*` (routes + webhook adapters).
8. Delete tests: `refund_test.go`, `transport_update_test.go`, `export_test.go` (if unused).
9. Verify: `go build ./internal/module/order/...` green, then `go build ./...` (may still fail in unrelated modules — acceptable).
10. Manual smoke path: checkout wallet → seller confirm wallet → transport webhook Delivered → trigger escrow release method manually → payout Success.

## 8. Out of Scope

- Account, catalog, chat, common, promotion, analytic, inventory modules (stubs only where needed).
- Writing new tests.
- Metric/observability refactor.
- Restate journal serialization optimization.
- Gateway-side integrations (SEPay, VNPay, etc.) — new `tx.id → gateway ref` threading is defined but actual provider adapters are updated as a follow-up if their contracts change.

## 9. Risks & Open Items

- **Restate JSON round-trip**: we accept the risk of returning full sqlc structs in `restate.Run()` closures. If a bug surfaces in the journal (e.g., `uuid.UUID` becomes `[16]byte{...}` on unmarshal), narrow that specific closure to minimal struct.
- **Cross-module compile**: `account/biz/wallet.go` is currently broken at a schema level (references dropped columns). We stub `WalletDebit`/`WalletCredit` with current signatures but they will not function end-to-end until account module is refactored in a separate PR.
- **Webhook tx threading**: gateway providers must be told the `tx.id` at tx creation time. Existing adapters pass `ref_id`; if a provider only accepts its own reference, we store the provider ref in `tx.data` JSONB and maintain a lookup index. No schema change needed.
- **Escrow timer persistence**: Restate timers persist across restarts. Seven-day delay is within Restate's supported range. No concerns.
