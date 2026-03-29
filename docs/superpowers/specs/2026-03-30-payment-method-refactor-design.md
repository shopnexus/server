# Payment Method Refactor

## Goal

Refactor payment methods so buyers can save credit/debit cards (tokenized via a processor) for one-click payments, alongside existing redirect-based providers (VNPay, SePay, COD). Auto-refund to the original card when a refund is approved.

## Payment Method Categories

| Type | Examples | Status |
|------|----------|--------|
| `card` | Visa, Mastercard (credit/debit) | Implementing now |
| `ewallet` | ZaloPay, MoMo | TODO — future |
| `bank` | TPBank, Vietcombank | TODO — future |

Redirect-based providers (VNPay, SePay, COD) remain system-wide options, not per-account.

## Data Model

### `account.payment_method` — add `provider` column

```sql
ALTER TABLE "account"."payment_method"
  ADD COLUMN "provider" VARCHAR(50) NOT NULL DEFAULT '';
```

Columns after migration:

| Column | Type | Description |
|--------|------|-------------|
| id | UUID PK | |
| account_id | UUID FK | |
| type | VARCHAR(50) | `card` / `ewallet` / `bank` |
| provider | VARCHAR(50) | Processor that tokenized it, e.g. `stripe` |
| label | VARCHAR(100) | Auto-generated, e.g. `Visa ending in 4242` |
| data | JSONB | Provider token + card display info |
| is_default | BOOLEAN | At most one per account (unique partial index) |
| date_created | TIMESTAMPTZ | |
| date_updated | TIMESTAMPTZ | |

Card `data` shape:

```json
{
  "token": "tok_abc123",
  "brand": "visa",
  "last4": "4242",
  "exp_month": 12,
  "exp_year": 2028,
  "card_type": "credit"
}
```

### `order.payment` — add `payment_method_id` column

```sql
ALTER TABLE "order"."payment"
  ADD COLUMN "payment_method_id" UUID NULL
    REFERENCES "account"."payment_method"("id") ON DELETE SET NULL;
```

Set when the order was charged via a saved card. NULL for redirect-based payments. Used to look up the card token for refunds.

## `payment.Client` Interface

Add three methods to the existing interface:

```go
type Client interface {
    // Existing
    Config() sharedmodel.OptionConfig
    Create(ctx context.Context, params CreateParams) (CreateResult, error)
    Get(ctx context.Context, providerID string) (PaymentInfo, error)
    OnResult(fn ResultHandler)
    InitializeWebhook(e *echo.Echo)

    // New
    Charge(ctx context.Context, params ChargeParams) (ChargeResult, error)
    Refund(ctx context.Context, params RefundParams) (RefundResult, error)
    Tokenize(ctx context.Context, params TokenizeParams) (TokenizeResult, error)
}
```

### New types

```go
type ChargeParams struct {
    Token       string      // payment method token from provider
    Amount      Concurrency
    Description string
}

type ChargeResult struct {
    ProviderChargeID string
    Status           Status // success or failed
}

type RefundParams struct {
    ProviderChargeID string
    Amount           Concurrency // supports partial refund
}

type RefundResult struct {
    ProviderRefundID string
    Status           Status
}

type TokenizeParams struct {
    AccountID uuid.UUID
    ReturnURL string
}

type TokenizeResult struct {
    FormURL      string          // for redirect-based tokenization
    ClientConfig json.RawMessage // for JS SDK-based tokenization (publishable key, etc.)
}
```

### Provider support matrix

| Method | COD | VNPay | SePay | Card Provider |
|--------|-----|-------|-------|---------------|
| Create | yes | yes | yes | no |
| Get | no-op | pending only | yes | no |
| Webhook | no-op | yes | yes | optional backup |
| Charge | no | no | no | yes |
| Refund | no | no | no | yes |
| Tokenize | no | no | no | yes |

Unsupported methods return `payment.ErrNotSupported`.

## Pay Flow (BE)

`PayOrders` receives `{ order_ids, payment_option }`. Branching logic:

### Case 1: Saved payment method (`payment_option` starts with `pm:`)

1. Parse UUID from `pm:<uuid>`
2. Look up `account.payment_method` by ID, verify `account_id` matches buyer
3. Find card provider in `paymentMap` by the method's `provider` field
4. Create `order.payment` record with `payment_method_id` set
5. Call `provider.Charge(token, amount)`
6. Charge is synchronous — update payment status to success/failed immediately
7. Return result (no redirect URL)

### Case 2: Redirect provider (option slug like `vnpay_qr`)

Existing flow, unchanged. `payment_method_id` is NULL.

### Case 3: Default (`payment_option` is empty or `"default"`)

1. Look up buyer's default `account.payment_method`
2. If found, treat as Case 1
3. If none, return error: "no default payment method"

## Refund Flow (BE)

When `ConfirmRefund` is called and the refund is approved:

1. Look up the order's payment record
2. If `payment_method_id` is set (card payment):
   - Get the charge ID from `payment.data`
   - Look up the card provider via the payment method's `provider`
   - Call `provider.Refund(chargeID, refundAmount)`
   - Update refund status based on result
3. If `payment_method_id` is NULL (redirect-based):
   - Skip auto-refund (TODO for future)

## Add Card Flow

### From account settings

1. FE calls `POST /api/v1/account/payment-method/tokenize`
2. BE calls card provider's `Tokenize()`, returns `FormURL` or `ClientConfig`
3. FE either redirects to `FormURL` or renders provider's JS SDK iframe
4. Buyer enters card details on provider's hosted form (never touches our server)
5. Provider returns token (via redirect query params or JS callback)
6. FE calls `POST /api/v1/account/payment-method` with `{ type: "card", provider: "stripe", token, brand, last4, exp_month, exp_year, card_type }`
7. BE saves to `account.payment_method`

### During pay flow

Same as above, but after saving the card, FE immediately calls `PayOrders` with `pm:<new-card-id>`.

## API Endpoints

### New

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/account/payment-method/tokenize` | Get hosted form config for card tokenization |

### Existing (need biz logic wiring)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/account/payment-method` | List buyer's saved methods |
| POST | `/api/v1/account/payment-method` | Save a tokenized card |
| PUT | `/api/v1/account/payment-method/:id/default` | Set as default |
| DELETE | `/api/v1/account/payment-method/:id` | Delete a saved method |

## Frontend Changes

### Pay dialog (order list + order detail pages)

Replace hardcoded `payment_option: "default"` with a payment method selector:

1. Fetch saved methods (`useListPaymentMethods`) + redirect options (`useListServiceOption({ category: "payment" })`)
2. Combined list:
   - Saved cards at top — brand icon, last 4, default badge. Sends `pm:<id>`
   - Redirect providers below — VNPay, SePay, COD. Sends option slug
   - "Add new card" button — opens tokenize flow, then pays immediately
3. Default card pre-selected if exists

### Account > Payment Methods page

- Remove `e-wallet` and `bank-transfer` from UI type selector
- Replace freeform JSON textarea with proper "Add Card" flow via `Tokenize()`
- Display cards with brand icon, last 4, expiry, default badge
- Actions: set as default, delete

### No changes to

- Checkout page (creates pending items, no payment step)
- Seller pages (don't interact with payment)

## Card Provider Implementation

A stub card provider is registered in `payment_map.go`, gated by config (same pattern as SePay — only registered when config is non-empty). The actual processor (Stripe, PayOS, etc.) is plugged in later by implementing the `Charge`, `Refund`, and `Tokenize` methods.

```go
// config/struct.go
type CardPayment struct {
    Provider  string `yaml:"provider"`   // "stripe", "payos", etc.
    SecretKey string `yaml:"secretKey"`
    PublicKey string `yaml:"publicKey"`  // for client-side SDK
}
```

## Out of Scope

- Auto-refund for redirect-based payments (VNPay, SePay, COD)
- E-wallet linking (ZaloPay, MoMo)
- Bank app linking (TPBank, Vietcombank)
- Actual card processor implementation (interface only, stub provider)
