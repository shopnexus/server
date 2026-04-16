# Order Flow Redesign: Pay-First Checkout

**Date:** 2026-04-16
**Status:** Approved
**Scope:** Rewrite checkout, seller confirmation, payment, and wallet flows

## Problem

The current flow requires seller confirmation before buyer can pay. This creates bad UX:
- Buyer waits indefinitely for seller to confirm
- Buyer doesn't know shipping cost at checkout
- Seller can game shipping fees by grouping orders
- No timeout mechanism for unresponsive sellers

## New Flow

```
CHECKOUT:
1. Buyer selects items + 1 address + transport option per item + payment option
2. Reserve inventory
3. Quote transport per item (individually, then sum)
4. Total = sum(product costs) + sum(transport costs)
5. Buyer opts in to wallet deduction (if desired)
6. Remaining > 0: create payment via provider (VNPay/SePay/COD)
7. Create pending items (linked to payment_id)
8. Start payment timeout (15 min)

PAYMENT CONFIRM (webhook or wallet-only):
1. Payment success: start seller timeout (48h)
2. Payment fail/timeout: cancel items + release inventory + refund wallet

SELLER:
1. Seller sees paid items (payment.status=Success, order_id=NULL, date_cancelled=NULL)
2. Seller groups items (must share same transport_option) and confirms
3. Platform creates order + transport (using buyer's chosen provider)
4. Reject: refund to wallet + set date_cancelled + release inventory
5. 48h timeout: auto-cancel + refund to wallet

DELIVERY:
1. GHTK webhook: transport status updates
2. Status chain: Pending -> LabelCreated -> InTransit -> OutForDelivery -> Delivered
```

## Key Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Address | Per checkout (1 for all items) | Simpler UX, matches Shopee |
| Transport option | Per item | Buyer chooses shipping speed per item |
| Payment timing | At checkout | Buyer pays immediately, no waiting |
| Inventory reserve | Before payment | Lock stock while buyer pays (15 min window) |
| Payment timeout | 15 min auto-cancel | Release inventory if buyer doesn't pay |
| Seller confirm timeout | 48h auto-cancel + refund wallet | Prevent indefinite waiting |
| Seller reject | Refund to wallet (internal balance) | Fast refund, no provider round-trip |
| Seller group constraint | Same transport_option only | Prevents gaming, simplifies grouping |
| Transport quote | Per item individually | Seller groups later, platform absorbs cost difference |
| Shipping fee ownership | Platform owns | Buyer pays estimate, platform pays actual to carrier |
| Wallet + provider | Combinable, buyer opts in | Wallet deducted first if buyer chooses, rest via provider |
| Item status | Inferred (no column) | Derived from payment.status -> order_id -> transport.status |

## Item Status (Inferred)

No `status` column on items. Status derived from relationships:

```
payment.status = Pending              -> PendingPayment
payment.status = Cancelled/Failed     -> Cancelled
payment.status = Success:
  +- date_cancelled IS NOT NULL       -> Rejected/Cancelled
  +- order_id IS NULL                 -> Pending (waiting for seller)
  +- order_id IS NOT NULL             -> Confirmed
       +- check order.transport.status -> delivery state
```

## Database Changes

### Wallet (in account schema)

```sql
CREATE TYPE account.wallet_transaction_type AS ENUM ('Refund', 'Payment', 'TopUp');

CREATE TABLE account.wallet (
    account_id  UUID PRIMARY KEY,
    balance     BIGINT NOT NULL DEFAULT 0,
    CHECK (balance >= 0)
);

CREATE TABLE account.wallet_transaction (
    id            BIGSERIAL PRIMARY KEY,
    account_id    UUID NOT NULL REFERENCES account.wallet(account_id),
    type          account.wallet_transaction_type NOT NULL,
    amount        BIGINT NOT NULL,
    reference_id  TEXT,
    note          TEXT,
    date_created  TIMESTAMPTZ(3) NOT NULL DEFAULT now()
);

CREATE INDEX idx_wallet_tx_account
    ON account.wallet_transaction(account_id, date_created DESC);
```

### Order module changes

```sql
-- Item table: add columns
ALTER TABLE "order".item ADD COLUMN transport_option TEXT;
ALTER TABLE "order".item ADD COLUMN transport_cost_estimate BIGINT NOT NULL DEFAULT 0;
ALTER TABLE "order".item ADD COLUMN payment_id BIGINT REFERENCES "order".payment(id);
ALTER TABLE "order".item ADD COLUMN date_cancelled TIMESTAMPTZ(3);

-- Item table: drop status column
ALTER TABLE "order".item DROP COLUMN status;

-- Drop item_status enum
DROP TYPE "order".item_status;

-- Order table: drop payment_id (payment lives on items now)
ALTER TABLE "order".order DROP COLUMN payment_id;

-- Indexes
CREATE INDEX idx_item_payment ON "order".item(payment_id) WHERE payment_id IS NOT NULL;
CREATE INDEX idx_item_seller_pending ON "order".item(seller_id, transport_option)
    WHERE order_id IS NULL AND date_cancelled IS NULL;
```

### Columns removed
- `order.payment_id` — payment link moved to items
- `item.status` — inferred from payment + order + transport

### Columns added to item
- `transport_option TEXT` — buyer's chosen transport provider
- `transport_cost_estimate BIGINT` — quoted shipping cost (for accurate refund)
- `payment_id BIGINT` — link to checkout payment
- `date_cancelled TIMESTAMPTZ(3)` — set when rejected or timed out

### Semantic changes (existing columns)
- `item.paid_amount` — full product price (unit_price * quantity). The wallet vs provider split is NOT tracked per item; it is tracked at payment level. When refunding a rejected item, the full `paid_amount + transport_cost_estimate` is credited to the wallet regardless of original payment source.
- `item.address` — per checkout (all items share same address)
- `order.product_discount` — always 0 (buyer pays full at checkout)
- `order.transport_cost` — actual GHTK cost (may differ from buyer's estimate; platform absorbs the difference as profit or loss)

### Unchanged
- `payment` table structure
- `transport` table structure
- `order` table structure (except payment_id removed)
- `refund`, `refund_dispute` tables

## API Changes

### Checkout

```go
type BuyerCheckoutParams struct {
    Account       accountmodel.AuthenticatedAccount
    BuyNow        bool
    Address       string        // single address for all items
    PaymentOption string        // "vnpay", "sepay", "cod"
    UseWallet     bool          // buyer explicitly opts in to wallet deduction
    Items         []CheckoutItem
}

type CheckoutItem struct {
    SkuID           uuid.UUID
    Quantity        int64
    TransportOption string      // "ghtk_standard", "ghtk_express", etc.
    Note            string
}

type BuyerCheckoutResult struct {
    Items          []ordermodel.OrderItem
    Payment        *ordermodel.Payment   // nil if wallet covers everything
    RedirectUrl    *string               // VNPay/SePay redirect URL
    WalletDeducted int64                 // amount deducted from wallet
    Total          int64
}
```

### Checkout execution (8 steps)

1. Validate params
2. Fetch products from catalog (prices, seller_ids, sku names)
3. Reserve inventory per item
4. Quote transport per item individually, sum costs
5. Calculate total = sum(product) + sum(transport)
6. Process payment:
   - If UseWallet: wallet_deducted = wallet.Debit(min(balance, total))
   - Else: wallet_deducted = 0
   - remaining = total - wallet_deducted
   - If remaining > 0: create Payment + provider redirect
   - If remaining == 0: create Payment with status Success (wallet-only)
7. Create items linked to payment_id with transport_option and transport_cost_estimate
8. If needs provider: start 15 min timeout (CancelUnpaidCheckout)
   If wallet-only: start 48h seller timeout (AutoCancelPendingItems)

### Seller Confirm (simplified)

```go
type ConfirmSellerPendingParams struct {
    Account accountmodel.AuthenticatedAccount // seller
    ItemIDs []int64
    Note    string
}
```

Execution:
1. Validate: items have payment.status=Success, order_id=NULL, date_cancelled=NULL, same seller, same transport_option
2. Get seller contact address
3. Platform creates transport using buyer's transport_option
4. Create order (product_cost + actual transport_cost from GHTK)
5. Set order_id on items
6. Notify buyer

### Seller Reject

Execution:
1. Validate items (same checks as confirm)
2. Release inventory per item
3. Refund to wallet: credit(paid_amount + transport_cost_estimate) per item
4. Set date_cancelled = now() on items
5. Notify buyer

### Removed endpoints
- `QuoteTransport` — buyer quotes at checkout, seller doesn't need this

### Wallet (added to AccountBiz)

```go
GetWalletBalance(ctx context.Context, accountID uuid.UUID) (int64, error)
WalletDebit(ctx context.Context, params WalletDebitParams) (int64, error)   // returns actual deducted
WalletCredit(ctx context.Context, params WalletCreditParams) error
```

### Timeout Handlers

**CancelUnpaidCheckout (15 min):**
1. Check items still linked to pending payment
2. Cancel payment
3. Release inventory per item
4. Refund wallet (credit back wallet_deducted)
5. Notify buyer: payment timeout

**AutoCancelPendingItems (48h):**
1. Check items: payment.status=Success, order_id=NULL, date_cancelled=NULL
2. Release inventory per item
3. Refund to wallet: credit(paid_amount + transport_cost_estimate)
4. Set date_cancelled = now()
5. Notify buyer + seller: auto-cancelled

**ConfirmPaymentForCheckout (webhook):**
1. Payment webhook confirms success
2. Find items by payment_id
3. Start 48h seller timeout per seller group
4. Notify buyer: payment successful, waiting for seller

## Error Handling & Race Conditions

### Race conditions

**Double checkout:** Restate idempotency key = `checkout:{account_id}:{hash(item_ids)}`. Duplicate request replays from journal.

**Seller confirm + auto-cancel race:** Both check `order_id IS NULL AND date_cancelled IS NULL`. SQL WHERE clause means only one succeeds (0 rows updated = skip).

**Payment webhook + timeout race:** Both check payment status. Only one transitions payment; the other finds it already transitioned and skips.

**Seller reject partial items:** Seller can reject some items and confirm others (different transport_option groups). No conflict since they operate on different item_ids.

### Failure scenarios

**GHTK Quote fail at checkout:** No items created, no inventory reserved. Return error, buyer retries.

**Payment provider fail:** Items created as PendingPayment, inventory reserved. 15 min timeout auto-cancels and releases everything.

**Wallet debit succeeds but provider fails:** 15 min timeout refunds wallet + releases inventory. Buyer loses nothing.

**GHTK Create fail at seller confirm:** Order not created, items stay Pending. Seller retries. If no retry, 48h timeout cancels and refunds.

**Inventory reserve fail (out of stock):** Checkout fails at step 3. Nothing created, nothing to clean up.

**Wallet Credit fail on refund:** Wrapped in restate.Run() with auto-retry. Restate guarantees exactly-once.

### Consistency guarantees

| Operation | Guarantee |
|---|---|
| Reserve -> Create items | Restate journal: crash replays skip completed steps |
| Wallet debit -> Payment create | Restate journal: crash replays skip debit |
| Timeout cancel | Restate delayed call: guaranteed delivery, exactly-once |
| Webhook confirm | Restate handler: idempotent replay |
