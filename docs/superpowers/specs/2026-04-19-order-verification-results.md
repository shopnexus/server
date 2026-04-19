# Order Feature Verification Results

**Date**: 2026-04-19
**Scope**: Full order lifecycle (BE + FE) including currency migration
**Method**: Concern-Based Review Matrix

## Summary Matrix

| Flow \ Concern | C1 Data | C2 Error | C3 Race | C4 FE↔BE | C5 Currency | C6 State | C7 Edge | C8 Auth |
|---|---|---|---|---|---|---|---|---|
| **F1 Cart** | ⚠️ | ⚠️ | ✅ | ⚠️ | ✅ | ✅ | ⚠️ | ✅ |
| **F2 Checkout** | ⚠️ | 🔴 | 🔴 | ⚠️ | 🔴 | ⚠️ | 🔴 | ⚠️ |
| **F3 Payment** | ⚠️ | 🔴 | 🔴 | ⚠️ | 🔴 | 🔴 | ⚠️ | ⚠️ |
| **F4 Seller Pending** | ⚠️ | ⚠️ | 🔴 | ⚠️ | ✅ | ⚠️ | ⚠️ | 🔴 |
| **F5 Order Queries** | ⚠️ | ✅ | ✅ | ⚠️ | ⚠️ | ✅ | ✅ | 🔴 |
| **F6 Transport** | 🔴 | ⚠️ | 🔴 | ⚠️ | ⚠️ | ⚠️ | ⚠️ | 🔴 |
| **F7 Refund** | 🔴 | ⚠️ | 🔴 | 🔴 | ⚠️ | 🔴 | ⚠️ | 🔴 |
| **F8 Dispute** | ✅ | ✅ | ⚠️ | 🔴 | ✅ | ⚠️ | ✅ | ⚠️ |
| **F9 Timeout** | ✅ | ⚠️ | ⚠️ | ✅ | ⚠️ | ⚠️ | ⚠️ | ✅ |
| **F10 Dashboard** | 🔴 | 🔴 | ⚠️ | 🔴 | ⚠️ | ✅ | ⚠️ | ✅ |

Legend: ✅ OK | ⚠️ Medium/Low issues | 🔴 Critical/High issues

## Statistics

| Severity | Count |
|----------|-------|
| CRITICAL | 9 |
| HIGH | 18 |
| MEDIUM | 23 |
| LOW | 15 |
| **Total** | **65** |

---

## CRITICAL Findings (9)

### F2-C2-1 | Wallet not rolled back on checkout failure
- **File**: `biz/checkout.go:163-174`
- **Description**: If `WalletDebit` succeeds but a subsequent step fails (payment provider, item creation), the wallet deduction is never rolled back. Buyer loses money with no items created.
- **Fix**: Add deferred compensation — credit wallet back if function returns error after successful debit.

### F4-C8-1 | No ownership check on GetBuyerOrder / GetSellerOrder
- **File**: `biz/order.go:25-49`
- **Description**: Any authenticated user can fetch any order by UUID. `GetSellerOrder` delegates to `GetBuyerOrder` with no seller_id check. Transport layer extracts claims but discards them.
- **Fix**: Pass AccountID to biz methods, verify `order.BuyerID == accountID` or `order.SellerID == accountID`.

### F6-C8-1 | Transport webhook pipeline completely non-functional
- **File**: `transport/echo/order.go:77-89`
- **Description**: Transport webhook handlers (`OnResult`, `InitializeWebhook`) are never wired up in `NewHandler`. The entire transport webhook pipeline is dead code. Transport status never updates from webhooks.
- **Fix**: Add transport webhook wiring in `NewHandler`, mirroring the payment pattern.

### F6-C1-1 | Transport ID type mismatch (string vs UUID)
- **File**: `provider/transport/ghtk/ghtk.go:288-289`
- **Description**: `WebhookResult.TransportID` is set to GHTK's label ID (e.g., "GTK12345678") but `UpdateTransportStatus` expects `uuid.UUID`. UUID parse will always fail.
- **Fix**: Add lookup query `GetTransportByTrackingID` or store mapping between provider IDs and transport UUIDs.

### F7-C8-1 | ListBuyerRefunds returns ALL refunds to any user
- **File**: `biz/refund.go:30-83`
- **Description**: No account_id filter in `ListBuyerRefunds`. Any authenticated user sees ALL refunds in the entire system.
- **Fix**: Add `AccountID` to params and filter in DB query.

### F7-C8-2 | CancelBuyerRefund has no ownership check
- **File**: `biz/refund.go:269-318`
- **Description**: Any authenticated user can cancel any other user's refund.
- **Fix**: After GetRefund, check `refund.AccountID == params.Account.ID`.

### F7-C8-3 | UpdateBuyerRefund has no ownership check
- **File**: `biz/refund.go:196-217`
- **Description**: Any authenticated user can modify any pending refund.
- **Fix**: After GetRefund, check `refund.AccountID == params.Account.ID`.

### F10-C1-1 | Dashboard queries reference non-existent columns
- **File**: `db/queries/dashboard_custom.sql:14,17,30,40,55`
- **Description**: Multiple dashboard queries reference `o."status"` on `order."order"` table and `it."status"` on `order."item"` table — neither column exists. All dashboard queries will fail at runtime.
- **Fix**: Remove `status` filters or replace with correct column expressions (e.g., `it."order_id" IS NOT NULL` for confirmed items).

### F10-C1-2/3 | (Part of F10-C1-1 — same root cause)

---

## HIGH Findings (18)

### F2-C2-2 | No inventory release on checkout failure
- **File**: `biz/checkout.go:80-92`
- **Description**: If inventory reserve succeeds but subsequent steps fail, reserved stock is permanently locked.
- **Fix**: Deferred compensation to release inventory on error after successful reservation.

### F2-C3-1 | No idempotency protection on checkout
- **File**: `biz/checkout.go:38-443`
- **Description**: Double-submit can cause double inventory reservation, double wallet debit, duplicate items. FE disables button but is client-side only.
- **Fix**: Implement idempotency key or use Restate keyed service per buyer.

### F2-C5-1 | formatPrice uses USD but amounts are VND
- **File**: `shopnexus-website-v3/lib/utils.ts:8-13`
- **Description**: `formatPrice` uses `Intl.NumberFormat("en-US", { currency: "USD" })` but all amounts are in VND. Every price in the entire FE displays incorrectly.
- **Fix**: Change to VND formatter or make currency configurable.

### F2-C7-1 | No overflow protection on price * quantity
- **File**: `biz/checkout.go:148-154`
- **Description**: `int64(sku.Price) * item.Quantity` can overflow silently. No max validation on Quantity.
- **Fix**: Add `max=100000` on CheckoutItem.Quantity, add overflow detection.

### F3-C2-1 | ConfirmPayment not idempotent
- **File**: `biz/order.go:336-354`
- **Description**: Duplicate webhook fires can schedule multiple auto-cancel timers and overwrite Success with Failed.
- **Fix**: Check current status before update, return early if not Pending.

### F3-C3-1 | ConfirmPayment races with CancelUnpaidCheckout
- **File**: `biz/order.go:336` + `biz/timeout.go:33`
- **Description**: Webhook and timeout can run concurrently. Timeout cancels items that were just confirmed as paid.
- **Fix**: Check payment status in CancelUnpaidCheckout before cancelling.

### F3-C5-1 | VNPay hardcoded *27000 exchange rate
- **File**: `provider/payment/vnpay/vnpay.go:86`
- **Description**: `Amount * 100 * 27000` — if amounts are already in VND, this inflates by 27000x.
- **Fix**: Remove *27000 for VND amounts, use exchange rate from payment record.

### F3-C6-1 | No state transition guards on payment status
- **File**: `biz/order.go:322-329`
- **Description**: UpdatePayment uses COALESCE, allowing any status to overwrite any other (e.g., Success → Failed).
- **Fix**: Add `WHERE status = 'Pending'` to UpdatePayment query.

### F4-C3-1 | TOCTOU race in seller confirm
- **File**: `biz/incoming.go:111-178`
- **Description**: Items validated in step 1, but order created and items linked in later steps. Concurrent cancel can make items disappear. SetItemsOrderID row count not checked.
- **Fix**: Check rowsAffected from SetItemsOrderID, verify it equals expected count.

### F4-C8-2 | No role-based access on seller endpoints
- **File**: `transport/echo/order.go:175-199`
- **Description**: Any authenticated user (including buyers) can hit `/seller/` endpoints. No role middleware.
- **Fix**: Add seller-role middleware to `/seller/` route group.

### F5-C8-3 | GetBuyerOrder transport layer discards claims
- **File**: `transport/echo/order.go:98-117`
- **Description**: Same as F4-C8-1 — claims extracted but account ID never passed to biz layer.
- **Fix**: Pass claims.Account.ID to biz method.

### F6-C3-1 | Race condition in transport status update
- **File**: `biz/transport_update.go:73-125`
- **Description**: Read-validate-write not atomic. Concurrent webhooks can both read same status.
- **Fix**: Use SELECT FOR UPDATE or WHERE status = $current in UPDATE.

### F6-C8-2 | GHTK webhook has no signature verification
- **File**: `provider/transport/ghtk/ghtk.go:256-260`
- **Description**: POST webhook endpoint with no authentication. Any caller can post fake transport updates.
- **Fix**: Implement HMAC signature verification.

### F6-C8-3 | Transport API keys hardcoded as placeholders
- **File**: `biz/transport_map.go:20`
- **Description**: `"your-api-key"` and `"your-client-id"` hardcoded. Will fail in production.
- **Fix**: Load from config/environment variables.

### F7-C8-4 | ConfirmSellerRefund has no seller check
- **File**: `biz/refund.go:321-392`
- **Description**: Any authenticated user can confirm (approve) any refund, triggering payment provider refund.
- **Fix**: Verify params.Account.ID == order.SellerID before confirming.

### F7-C6-1 | CreateBuyerRefund doesn't validate order state
- **File**: `biz/refund.go:86-183`
- **Description**: No check that order exists, belongs to buyer, or is in refundable state. Buyer can refund pending/cancelled orders or other users' orders.
- **Fix**: Fetch order, verify ownership and status before creating refund.

### F7-C1-1 | dbToRefund drops ItemIDs and Amount
- **File**: `biz/refund.go:240-266`
- **Description**: Partial refund fields never mapped from DB to model. Clients never see refund items or amount. Auto-refund uses wrong amount.
- **Fix**: Add JSON deserialization of ItemIDs and Amount mapping in dbToRefund.

### F7-C4-1 | FE TRefund missing item_ids and amount
- **File**: `core/order/refund.buyer.ts:15-27`
- **Description**: FE type lacks partial refund fields. Even after BE fix, FE discards them.
- **Fix**: Add `item_ids: number[] | null` and `amount: number` to TRefund.

### F10-C2-1 | Dashboard methods skip validation
- **File**: `biz/dashboard.go:66-83`
- **Description**: No `validator.Validate(params)` despite validate struct tags. Invalid params pass to DB.
- **Fix**: Add validation calls to all 4 dashboard methods.

### F10-C4-1 | Dashboard pending_actions loses breakdown
- **File**: `analytic/model/dashboard.go:26`
- **Description**: FE gets single number for pending actions, can't distinguish items vs refunds.
- **Fix**: Return pending_items and pending_refunds separately.

---

## MEDIUM Findings (23)

| ID | Flow | Description |
|----|------|-------------|
| F1-C1-1 | Cart | FE `CartItem.resource` typed non-nullable but BE returns null |
| F2-C1-2 | Checkout | Currency columns (buyer/seller/exchange_rate) never mapped to model or FE |
| F2-C2-3 | Checkout | `getPaymentClient` error not wrapped with context |
| F2-C4-1 | Checkout | Cart page hardcodes fake $5.99 shipping, disconnected from BE |
| F2-C4-2 | Checkout | Checkout page shows $0 shipping, doesn't include transport in total |
| F2-C5-2 | Checkout | Payment creation relies on DB defaults for currency, not explicit |
| F2-C5-3 | Checkout | Price * quantity multiplication has no overflow check |
| F2-C6-1 | Checkout | FE checkout button doesn't check cart.length > 0 |
| F2-C8-2 | Checkout | GetBuyerOrder no ownership verification |
| F3-C1-1 | Payment | dbToPayment drops currency fields |
| F3-C5-2 | Payment | CreateDefaultPayment doesn't set currency params |
| F3-C7-1 | Payment | ConfirmPayment doesn't check date_expired |
| F3-C8-1 | Payment | ConfirmPayment Restate method has no auth guard |
| F4-C1-1 | Seller | Order total uses gross price, discount hardcoded to 0 |
| F4-C2-1 | Seller | Batch error doesn't identify which item failed |
| F4-C3-2 | Seller | Reject race: cancel count not checked, potential double refund |
| F4-C3-3 | Seller | Orphaned transport on order creation failure |
| F4-C7-1 | Seller | FE "Select All" crosses group boundaries |
| F6-C2-1 | Transport | DB error masked as terminal, prevents Restate retry |
| F6-C2-2 | Transport | uuid.Parse errors silently ignored in notifications |
| F6-C4-1 | Transport | FE progress tracker missing LabelCreated step |
| F6-C5-1 | Transport | GHTK cost divided by hardcoded 27000 exchange rate |
| F7-C3-1 | Refund | Confirm vs cancel race: no row lock, last write wins |
| F7-C3-2 | Refund | No duplicate refund check per order |
| F7-C2-1 | Refund | Auto-refund swallows payment provider errors |
| F7-C2-2 | Refund | Refund amount not validated against order total |
| F7-C7-1 | Refund | Item IDs not validated against order's actual items |
| F7-C4-2 | Refund | Partial refund fields not in transport layer or FE |
| F7-C4-3 | Refund | FE seller refund hooks call non-existent endpoints |
| F7-C5-1 | Refund | Auto-refund uses 0 amount due to dbToRefund bug |
| F8-C3-1 | Dispute | Count + insert in separate Run blocks, race window |
| F9-C3-1 | Timeout | AutoCancel may refund items seller is confirming |
| F9-C2-1 | Timeout | CancelUnpaidCheckout partial failure leaves inventory locked |
| F9-C3-2 | Timeout | CancelUnpaid + CancelBuyerPending double refund race |
| F9-C7-1 | Timeout | WalletDebit with amount=0 fails validation |
| F10-C5-1 | Dashboard | Revenue uses different bases (order total vs paid_amount) |

## LOW Findings (15)

| ID | Flow | Description |
|----|------|-------------|
| F2-C1-3 | Checkout | FE TPayment.id is string, BE is int64 |
| F2-C1-4 | Checkout | OrderItem.Resources nil → JSON null, FE expects array |
| F1-C2-4 | Cart | DeltaQuantity only supports positive increments |
| F2-C6-2 | Checkout | No explicit "payment pending" state on items |
| F1-C7-5 | Cart | Deleted SKUs in cart show as zero-value items |
| F3-C4-1 | Payment | No FE page for payment gateway return |
| F3-C1-2 | Payment | Malformed RefID not logged for debugging |
| F3-C2-2 | Payment | VNPay webhook returns 200 even on error |
| F9-C6-1 | Timeout | CancelUnpaidCheckout doesn't check payment status first |
| F4-C6-1 | Seller | Confirm doesn't verify payment status is Success |
| F4-C2-2 | Seller | Reject assumes single buyer, only refunds first buyer |
| F4-C7-2 | Seller | FE confirm dialog shows gross price not paid_amount |
| F5-C4-1 | Order | FE payment ID type mismatch string vs number |
| F5-C4-2 | Order | Missing currency fields in FE TPayment type |
| F5-C1-1 | Order | Only first payment shown per order |
| F5-C4-3 | Order | Search not exposed in FE hooks |
| F5-C5-1 | Order | No currency column on order table |
| F4-C7-3 | Seller | No upper bound on ItemIDs array size |
| F6-C6-1 | Transport | Failed status cannot transition for redelivery |
| F6-C7-1 | Transport | Unrecognized webhook status silently dropped |
| F7-C2-3 | Refund | GetBuyerOrder error silently discarded in confirm |
| F7-C7-2 | Refund | Minimal test coverage |
| F8-C6-1 | Dispute | Disputes allowed on Failed refunds |
| F8-C4-1 | Dispute | Zero FE implementation |
| F8-C8-1 | Dispute | Wrong notification type (refund instead of dispute) |
| F10-C3-1 | Dashboard | Sequential RPC calls, no parallelization |
| F10-C7-1 | Dashboard | All comparisons nil for new sellers |

---

## Priority Remediation Order

### Phase 1: Security & Money (CRITICAL + HIGH auth/money issues)
1. Fix all authorization gaps (F4-C8-1, F5-C8-3, F7-C8-1/2/3/4, F4-C8-2)
2. Add checkout rollback for wallet + inventory (F2-C2-1, F2-C2-2)
3. Fix ConfirmPayment idempotency + race with timeout (F3-C2-1, F3-C3-1, F3-C6-1)
4. Fix VNPay *27000 multiplier (F3-C5-1)
5. Fix formatPrice USD → VND (F2-C5-1)
6. Add checkout idempotency (F2-C3-1)

### Phase 2: Data Integrity
7. Fix dashboard broken queries (F10-C1-1/2/3)
8. Fix dbToRefund missing fields (F7-C1-1)
9. Fix TOCTOU races in seller confirm/reject (F4-C3-1, F4-C3-2)
10. Fix refund state machine (F7-C6-1, F7-C6-2)
11. Add overflow protection (F2-C7-1)

### Phase 3: Transport Pipeline
12. Wire up transport webhooks (F6-C8-1)
13. Fix transport ID mapping (F6-C1-1)
14. Add GHTK webhook auth (F6-C8-2)
15. Load API keys from config (F6-C8-3)

### Phase 4: FE Consistency & Polish
16. Fix FE type mismatches (F2-C1-2, F7-C4-1/2, F5-C4-1)
17. Add partial refund UI (F7-C4-2)
18. Fix cart/checkout pricing display (F2-C4-1, F2-C4-2)
19. Add dispute FE (F8-C4-1)
20. Fix remaining MEDIUM/LOW issues
