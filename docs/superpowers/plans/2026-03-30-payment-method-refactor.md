# Payment Method Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable buyers to save credit/debit cards for one-click payments and auto-refund, alongside existing redirect-based providers.

**Architecture:** Extend `payment.Client` interface with `Charge`, `Refund`, `Tokenize` methods. Add `provider` column to `account.payment_method`, add `payment_method_id` FK to `order.payment`. Branch `PayOrders` logic based on `pm:` prefix vs redirect option slug. Stub card provider implements the new methods; existing providers return `ErrNotSupported`.

**Tech Stack:** Go/Restate (BE), PostgreSQL + SQLC (DB), Next.js + TanStack Query (FE), Echo (HTTP)

---

## File Map

### Backend — New files
- `internal/provider/payment/card/card.go` — stub card provider implementing `payment.Client`

### Backend — Modified files
- `internal/provider/payment/payment.go` — add `Charge/Refund/Tokenize` types + methods to `Client` interface
- `internal/provider/payment/cod/cod.go` — add `ErrNotSupported` stubs for new methods
- `internal/provider/payment/vnpay/vnpay.go` — add `ErrNotSupported` stubs for new methods
- `internal/provider/payment/sepay/sepay.go` — add `ErrNotSupported` stubs for new methods
- `internal/module/account/db/migrations/0001_init.up.sql` — add `provider` column to `payment_method`
- `internal/module/account/db/queries/account_payment_method.sql` — regenerate with `provider` column
- `internal/module/account/biz/interface.go` — add payment method biz methods to `AccountBiz`
- `internal/module/account/biz/payment_method.go` — new biz logic file for payment method operations
- `internal/module/account/transport/echo/payment_method.go` — new HTTP handler file for payment method endpoints
- `internal/module/account/transport/echo/account.go` — register new routes
- `internal/module/order/db/migrations/0001_init.up.sql` — add `payment_method_id` to `order.payment`
- `internal/module/order/db/queries/order_payment.sql` — regenerate with new column
- `internal/module/order/model/order.go` — add `PaymentMethodID` to `Payment` model
- `internal/module/order/biz/interface.go` — update `PayOrdersParams` + add `PaymentMethodID`
- `internal/module/order/biz/pay.go` — add card charge branching logic
- `internal/module/order/biz/refund.go` — add auto-refund for card payments
- `internal/module/order/biz/payment_map.go` — register stub card provider
- `config/struct.go` — add `CardPayment` config
- `config/config.default.yml` — add `cardPayment` defaults

### Frontend — Modified files
- `core/account/payment-method.ts` — update `PaymentMethod` type with `provider`, add `useTokenize` hook
- `core/order/order.buyer.ts` — update `usePayOrders` to accept payment method selection
- `app/(marketplace)/account/payment/page.tsx` — redesign with proper card display
- `app/(marketplace)/account/orders/page.tsx` — add payment method selector to pay dialog
- `app/(marketplace)/account/orders/[id]/page.tsx` — add payment method selector to pay dialog

---

### Task 1: DB Migrations — Add `provider` column and `payment_method_id` FK

**Files:**
- Modify: `internal/module/account/db/migrations/0001_init.up.sql`
- Modify: `internal/module/order/db/migrations/0001_init.up.sql`

- [ ] **Step 1: Add `provider` column to `account.payment_method`**

In `internal/module/account/db/migrations/0001_init.up.sql`, update the `payment_method` CREATE TABLE to include `provider`:

```sql
CREATE TABLE IF NOT EXISTS "account"."payment_method" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "type" VARCHAR(50) NOT NULL,
    "provider" VARCHAR(50) NOT NULL DEFAULT '',
    "label" VARCHAR(100) NOT NULL,
    "data" JSONB NOT NULL,
    "is_default" BOOLEAN NOT NULL DEFAULT false,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "payment_method_pkey" PRIMARY KEY ("id")
);
```

- [ ] **Step 2: Add `payment_method_id` column to `order.payment`**

In `internal/module/order/db/migrations/0001_init.up.sql`, add `payment_method_id` to the `payment` table:

```sql
CREATE TABLE IF NOT EXISTS "order"."payment" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "option" TEXT NOT NULL,
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
    "amount" BIGINT NOT NULL,
    "data" JSONB NOT NULL,
    "payment_method_id" UUID,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_paid" TIMESTAMPTZ(3),
    "date_expired" TIMESTAMPTZ(3) NOT NULL,
    CONSTRAINT "payment_pkey" PRIMARY KEY ("id")
);
```

- [ ] **Step 3: Regenerate SQLC queries for both modules**

```bash
go run ./cmd/pgtempl/ -module account -skip-schema-prefix
go run ./cmd/pgtempl/ -module order -skip-schema-prefix
sqlc generate
```

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/module/account/db/ internal/module/order/db/ sqlc.yaml
git commit -m "add provider column to payment_method and payment_method_id to order payment"
```

---

### Task 2: Extend `payment.Client` Interface

**Files:**
- Modify: `internal/provider/payment/payment.go`

- [ ] **Step 1: Add new types and `ErrNotSupported`**

Add these types and the sentinel error to `internal/provider/payment/payment.go`:

```go
import (
    "context"
    "encoding/json"
    "errors"

    sharedmodel "shopnexus-server/internal/shared/model"

    "github.com/google/uuid"
    "github.com/labstack/echo/v4"
)

var ErrNotSupported = errors.New("operation not supported by this payment provider")

// ... existing types ...

type ChargeParams struct {
    Token       string
    Amount      sharedmodel.Concurrency
    Description string
}

type ChargeResult struct {
    ProviderChargeID string
    Status           Status
}

type RefundParams struct {
    ProviderChargeID string
    Amount           sharedmodel.Concurrency
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
    FormURL      string          `json:"form_url,omitempty"`
    ClientConfig json.RawMessage `json:"client_config,omitempty"`
}
```

- [ ] **Step 2: Add new methods to `Client` interface**

```go
type Client interface {
    Config() sharedmodel.OptionConfig
    Create(ctx context.Context, params CreateParams) (CreateResult, error)
    Get(ctx context.Context, providerID string) (PaymentInfo, error)
    OnResult(fn ResultHandler)
    InitializeWebhook(e *echo.Echo)

    Charge(ctx context.Context, params ChargeParams) (ChargeResult, error)
    Refund(ctx context.Context, params RefundParams) (RefundResult, error)
    Tokenize(ctx context.Context, params TokenizeParams) (TokenizeResult, error)
}
```

- [ ] **Step 3: Verify build fails (existing providers don't implement new methods)**

```bash
go build ./...
```

Expected: compile errors in cod, vnpay, sepay packages.

- [ ] **Step 4: Commit**

```bash
git add internal/provider/payment/payment.go
git commit -m "add Charge, Refund, Tokenize to payment.Client interface"
```

---

### Task 3: Add `ErrNotSupported` Stubs to Existing Providers

**Files:**
- Modify: `internal/provider/payment/cod/cod.go`
- Modify: `internal/provider/payment/vnpay/vnpay.go`
- Modify: `internal/provider/payment/sepay/sepay.go`

- [ ] **Step 1: Add stubs to COD**

Add to `internal/provider/payment/cod/cod.go`:

```go
func (c *ClientImpl) Charge(ctx context.Context, params payment.ChargeParams) (payment.ChargeResult, error) {
    return payment.ChargeResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) Refund(ctx context.Context, params payment.RefundParams) (payment.RefundResult, error) {
    return payment.RefundResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) Tokenize(ctx context.Context, params payment.TokenizeParams) (payment.TokenizeResult, error) {
    return payment.TokenizeResult{}, payment.ErrNotSupported
}
```

- [ ] **Step 2: Add stubs to VNPay**

Add the same three methods to `internal/provider/payment/vnpay/vnpay.go` (identical signatures, return `payment.ErrNotSupported`).

- [ ] **Step 3: Add stubs to SePay**

Add the same three methods to `internal/provider/payment/sepay/sepay.go` (identical signatures, return `payment.ErrNotSupported`).

- [ ] **Step 4: Verify build passes**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/provider/payment/cod/ internal/provider/payment/vnpay/ internal/provider/payment/sepay/
git commit -m "add ErrNotSupported stubs for Charge, Refund, Tokenize to existing providers"
```

---

### Task 4: Create Stub Card Provider

**Files:**
- Create: `internal/provider/payment/card/card.go`
- Modify: `config/struct.go`
- Modify: `config/config.default.yml`
- Modify: `internal/module/order/biz/payment_map.go`

- [ ] **Step 1: Add config struct**

In `config/struct.go`, add after the `Sepay` field in `App`:

```go
CardPayment CardPayment `yaml:"cardPayment" mapstructure:"cardPayment"`
```

Add the struct:

```go
type CardPayment struct {
    Provider  string `yaml:"provider" mapstructure:"provider"`
    SecretKey string `yaml:"secretKey" mapstructure:"secretKey"`
    PublicKey string `yaml:"publicKey" mapstructure:"publicKey"`
}
```

- [ ] **Step 2: Add config defaults**

In `config/config.default.yml`, add under `app:`:

```yaml
  cardPayment:
    provider: ""
    secretKey: ""
    publicKey: ""
```

- [ ] **Step 3: Create stub card provider**

Create `internal/provider/payment/card/card.go`:

```go
package card

import (
    "context"
    "encoding/json"
    "fmt"

    "shopnexus-server/internal/provider/payment"
    sharedmodel "shopnexus-server/internal/shared/model"

    "github.com/labstack/echo/v4"
)

var _ payment.Client = (*ClientImpl)(nil)

type ClientImpl struct {
    config    sharedmodel.OptionConfig
    provider  string
    secretKey string
    publicKey string
    handlers  []payment.ResultHandler
}

type ClientOptions struct {
    Provider  string
    SecretKey string
    PublicKey string
}

func NewClient(cfg ClientOptions) *ClientImpl {
    return &ClientImpl{
        config: sharedmodel.OptionConfig{
            ID:       "card_" + cfg.Provider,
            Provider: cfg.Provider,
            Method:   "card",
            Name:     "Card Payment (" + cfg.Provider + ")",
        },
        provider:  cfg.Provider,
        secretKey: cfg.SecretKey,
        publicKey: cfg.PublicKey,
    }
}

func (c *ClientImpl) Config() sharedmodel.OptionConfig {
    return c.config
}

// Redirect-based methods — not supported for card payments
func (c *ClientImpl) Create(ctx context.Context, params payment.CreateParams) (payment.CreateResult, error) {
    return payment.CreateResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) Get(ctx context.Context, providerID string) (payment.PaymentInfo, error) {
    return payment.PaymentInfo{}, payment.ErrNotSupported
}

func (c *ClientImpl) OnResult(fn payment.ResultHandler) {
    c.handlers = append(c.handlers, fn)
}

func (c *ClientImpl) InitializeWebhook(e *echo.Echo) {
    // Card provider webhook is optional — charges are synchronous.
    // Implement when a specific processor is chosen.
}

// Card-specific methods — stub implementations until a real processor is configured
func (c *ClientImpl) Charge(ctx context.Context, params payment.ChargeParams) (payment.ChargeResult, error) {
    // TODO: implement with real processor (Stripe, PayOS, etc.)
    return payment.ChargeResult{}, fmt.Errorf("card provider %q: charge not implemented", c.provider)
}

func (c *ClientImpl) Refund(ctx context.Context, params payment.RefundParams) (payment.RefundResult, error) {
    // TODO: implement with real processor
    return payment.RefundResult{}, fmt.Errorf("card provider %q: refund not implemented", c.provider)
}

func (c *ClientImpl) Tokenize(ctx context.Context, params payment.TokenizeParams) (payment.TokenizeResult, error) {
    // TODO: implement with real processor
    // For now, return the public key so FE can render the processor's JS SDK
    return payment.TokenizeResult{
        ClientConfig: json.RawMessage(fmt.Sprintf(`{"provider":"%s","public_key":"%s"}`, c.provider, c.publicKey)),
    }, nil
}
```

- [ ] **Step 4: Register card provider in payment map**

In `internal/module/order/biz/payment_map.go`, add import and registration:

```go
import "shopnexus-server/internal/provider/payment/card"
```

Add after the SePay registration block:

```go
    // setup card payment client
    cardCfg := config.GetConfig().App.CardPayment
    if cardCfg.Provider != "" {
        cardClient := card.NewClient(card.ClientOptions{
            Provider:  cardCfg.Provider,
            SecretKey: cardCfg.SecretKey,
            PublicKey: cardCfg.PublicKey,
        })
        b.paymentMap[cardClient.Config().ID] = cardClient
        configs = append(configs, cardClient.Config())
    }
```

- [ ] **Step 5: Verify build**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/provider/payment/card/ config/struct.go config/config.default.yml internal/module/order/biz/payment_map.go
git commit -m "add stub card payment provider with config"
```

---

### Task 5: Update `Payment` Model and `PayOrders` Branching Logic

**Files:**
- Modify: `internal/module/order/model/order.go`
- Modify: `internal/module/order/biz/interface.go`
- Modify: `internal/module/order/biz/pay.go`
- Modify: `internal/module/order/biz/payment_map.go`

- [ ] **Step 1: Add `PaymentMethodID` to Payment model**

In `internal/module/order/model/order.go`, update the `Payment` struct:

```go
type Payment struct {
    ID              int64                   `json:"id"`
    AccountID       uuid.UUID               `json:"account_id"`
    Option          string                  `json:"option"`
    PaymentMethodID *uuid.UUID              `json:"payment_method_id"`
    Status          orderdb.OrderStatus     `json:"status"`
    Amount          sharedmodel.Concurrency `json:"amount"`
    Data            json.RawMessage         `json:"data"`
    DateCreated     time.Time               `json:"date_created"`
    DatePaid        *time.Time              `json:"date_paid"`
    DateExpired     time.Time               `json:"date_expired"`
}
```

- [ ] **Step 2: Add `getPaymentClientByProvider` helper**

In `internal/module/order/biz/payment_map.go`, add a new helper that finds a client by its provider name (used for card charges where the lookup key is the payment method's `provider`, not the option slug):

```go
func (b *OrderHandler) getPaymentClientByProvider(provider string) (payment.Client, error) {
    for _, client := range b.paymentMap {
        if client.Config().Provider == provider {
            return client, nil
        }
    }
    return nil, ordermodel.ErrUnknownPaymentOption.Fmt(provider).Terminal()
}
```

- [ ] **Step 3: Refactor `PayOrders` with branching logic**

Rewrite `internal/module/order/biz/pay.go` to handle three cases: `pm:` prefix (card charge), redirect option slug, and `"default"`:

```go
func (b *OrderHandler) PayOrders(ctx restate.Context, params PayOrdersParams) (PayOrdersResult, error) {
    var zero PayOrdersResult

    if err := validator.Validate(params); err != nil {
        return zero, sharedmodel.WrapErr("validate pay orders", err)
    }

    // Fetch orders and validate ownership + status (unchanged)
    type orderInfo struct {
        ID    string `json:"id"`
        Total int64  `json:"total"`
    }
    type fetchResult struct {
        Orders []orderInfo `json:"orders"`
    }

    fetched, err := restate.Run(ctx, func(ctx restate.RunContext) (fetchResult, error) {
        orders, err := b.storage.Querier().ListOrder(ctx, orderdb.ListOrderParams{
            ID: params.OrderIDs,
        })
        if err != nil {
            return fetchResult{}, sharedmodel.WrapErr("db list orders", err)
        }
        if len(orders) != len(params.OrderIDs) {
            return fetchResult{}, ordermodel.ErrOrderNotFound.Terminal()
        }

        var result []orderInfo
        for _, o := range orders {
            if o.BuyerID != params.Account.ID {
                return fetchResult{}, ordermodel.ErrOrderNotFound.Terminal()
            }
            if o.Status != orderdb.OrderStatusPending {
                return fetchResult{}, ordermodel.ErrOrderNotPayable
            }
            if o.PaymentID.Valid {
                return fetchResult{}, ordermodel.ErrOrderAlreadyPaid
            }
            result = append(result, orderInfo{
                ID:    o.ID.String(),
                Total: o.Total,
            })
        }
        return fetchResult{Orders: result}, nil
    })
    if err != nil {
        return zero, sharedmodel.WrapErr("fetch orders", err)
    }

    var totalAmount sharedmodel.Concurrency
    for _, o := range fetched.Orders {
        totalAmount += sharedmodel.Concurrency(o.Total)
    }

    // Resolve payment method
    paymentOption := params.PaymentOption
    if paymentOption == "" || paymentOption == "default" {
        // Look up default payment method
        pm, err := restate.Run(ctx, func(ctx restate.RunContext) (string, error) {
            methods, err := b.account.ListPaymentMethods(ctx, /* account_id, is_default=true */)
            // This depends on account biz — for now use direct DB query
            return "", err
        })
        if err != nil || pm == "" {
            return zero, ordermodel.ErrNoDefaultPaymentMethod.Terminal()
        }
        paymentOption = "pm:" + pm
    }

    // Branch: saved card vs redirect
    if strings.HasPrefix(paymentOption, "pm:") {
        return b.payWithSavedMethod(ctx, params, paymentOption[3:], totalAmount, fetched.Orders)
    }
    return b.payWithRedirect(ctx, params, paymentOption, totalAmount, fetched.Orders)
}
```

- [ ] **Step 4: Implement `payWithRedirect` (extract existing logic)**

```go
func (b *OrderHandler) payWithRedirect(ctx restate.Context, params PayOrdersParams, option string, totalAmount sharedmodel.Concurrency, orders []orderInfo) (PayOrdersResult, error) {
    var zero PayOrdersResult

    paymentClient, err := b.getPaymentClient(option)
    if err != nil {
        return zero, err
    }

    // Existing redirect flow — create payment record + call provider Create
    // (move the existing restate.Run blocks from the old PayOrders here, unchanged)
    // ...
}
```

- [ ] **Step 5: Implement `payWithSavedMethod` (new card charge flow)**

```go
func (b *OrderHandler) payWithSavedMethod(ctx restate.Context, params PayOrdersParams, paymentMethodID string, totalAmount sharedmodel.Concurrency, orders []orderInfo) (PayOrdersResult, error) {
    var zero PayOrdersResult

    pmUUID, err := uuid.Parse(paymentMethodID)
    if err != nil {
        return zero, sharedmodel.ErrValidation.Fmt("invalid payment method ID").Terminal()
    }

    // Fetch payment method + verify ownership
    type pmInfo struct {
        ID       string `json:"id"`
        Provider string `json:"provider"`
        Token    string `json:"token"`
    }

    pm, err := restate.Run(ctx, func(ctx restate.RunContext) (pmInfo, error) {
        dbPM, err := b.storage.Querier().GetPaymentMethod(ctx, /* id=pmUUID */)
        // Verify account_id matches, extract token from data
        // ...
    })

    // Get card provider
    cardClient, err := b.getPaymentClientByProvider(pm.Provider)

    // Create payment record with payment_method_id set
    // Call cardClient.Charge(token, amount)
    // Update payment status immediately based on charge result
    // Link payment to orders
    // Return result (no redirect URL)
}
```

Note: The exact DB query calls depend on the regenerated SQLC from Task 1. The engineer implementing this should reference the generated `Queries` interface after running `sqlc generate`.

- [ ] **Step 6: Add error sentinels**

In `internal/module/order/model/error.go` (or wherever `ErrOrderNotFound` etc. are defined), add:

```go
var ErrNoDefaultPaymentMethod = sharedmodel.NewError(http.StatusBadRequest, "no default payment method configured")
```

- [ ] **Step 7: Verify build**

```bash
go build ./...
```

- [ ] **Step 8: Commit**

```bash
git add internal/module/order/
git commit -m "add card charge branching to PayOrders"
```

---

### Task 6: Auto-Refund for Card Payments

**Files:**
- Modify: `internal/module/order/biz/refund.go`

- [ ] **Step 1: Read current `ConfirmRefund` implementation**

```bash
# Read the file to understand the current flow
```

- [ ] **Step 2: Add auto-refund logic after refund approval**

After the refund is confirmed/approved in `ConfirmRefund`, add:

```go
// After updating refund status to confirmed...

// Auto-refund for card payments
if order.Payment != nil && order.Payment.PaymentMethodID != nil {
    chargeID := "" // extract from order.Payment.Data["provider_charge_id"]

    // Look up card provider by payment method's provider
    cardClient, err := b.getPaymentClientByProvider(/* provider from payment method */)
    if err == nil {
        refundResult, err := cardClient.Refund(ctx, payment.RefundParams{
            ProviderChargeID: chargeID,
            Amount:           sharedmodel.Concurrency(refundAmount),
        })
        if err != nil {
            slog.Error("auto-refund failed", slog.Any("error", err))
            // Don't fail the confirm — log and continue, manual intervention needed
        }
        // Store refund provider ID in refund data if successful
    }
}
```

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/biz/refund.go
git commit -m "add auto-refund for card payments on refund confirmation"
```

---

### Task 7: Account Payment Method Biz + Transport

**Files:**
- Create: `internal/module/account/biz/payment_method.go`
- Create: `internal/module/account/transport/echo/payment_method.go`
- Modify: `internal/module/account/biz/interface.go`
- Modify: `internal/module/account/transport/echo/account.go`

- [ ] **Step 1: Add payment method methods to `AccountBiz` interface**

In `internal/module/account/biz/interface.go`, add to the `AccountBiz` interface:

```go
// Payment Methods
ListPaymentMethods(ctx context.Context, params ListPaymentMethodsParams) (sharedmodel.PaginateResult[accountdb.AccountPaymentMethod], error)
CreatePaymentMethod(ctx context.Context, params CreatePaymentMethodParams) (accountdb.AccountPaymentMethod, error)
SetDefaultPaymentMethod(ctx context.Context, params SetDefaultPaymentMethodParams) error
DeletePaymentMethod(ctx context.Context, params DeletePaymentMethodParams) error
TokenizeCard(ctx context.Context, params TokenizeCardParams) (payment.TokenizeResult, error)
```

- [ ] **Step 2: Create biz implementation**

Create `internal/module/account/biz/payment_method.go`:

```go
package accountbiz

import (
    restate "github.com/restatedev/sdk-go"
    "github.com/google/uuid"

    accountdb "shopnexus-server/internal/module/account/db/sqlc"
    "shopnexus-server/internal/provider/payment"
    sharedmodel "shopnexus-server/internal/shared/model"
    "shopnexus-server/internal/shared/validator"
)

type ListPaymentMethodsParams struct {
    AccountID uuid.UUID `validate:"required"`
    sharedmodel.PaginationParams
}

type CreatePaymentMethodParams struct {
    AccountID uuid.UUID `validate:"required"`
    Type      string    `json:"type" validate:"required,oneof=card ewallet bank"`
    Provider  string    `json:"provider" validate:"required"`
    Label     string    `json:"label" validate:"required,max=100"`
    Data      []byte    `json:"data" validate:"required"`
}

type SetDefaultPaymentMethodParams struct {
    AccountID       uuid.UUID `validate:"required"`
    PaymentMethodID uuid.UUID `validate:"required"`
}

type DeletePaymentMethodParams struct {
    AccountID       uuid.UUID `validate:"required"`
    PaymentMethodID uuid.UUID `validate:"required"`
}

type TokenizeCardParams struct {
    AccountID uuid.UUID `validate:"required"`
    ReturnURL string    `json:"return_url"`
}

func (h *AccountHandler) ListPaymentMethods(ctx restate.Context, params ListPaymentMethodsParams) (sharedmodel.PaginateResult[accountdb.AccountPaymentMethod], error) {
    // Query DB with account_id filter, return paginated
}

func (h *AccountHandler) CreatePaymentMethod(ctx restate.Context, params CreatePaymentMethodParams) (accountdb.AccountPaymentMethod, error) {
    // Validate, insert into DB via CreateDefaultPaymentMethod
}

func (h *AccountHandler) SetDefaultPaymentMethod(ctx restate.Context, params SetDefaultPaymentMethodParams) error {
    // Unset current default (UPDATE is_default=false WHERE account_id AND is_default=true)
    // Set new default (UPDATE is_default=true WHERE id AND account_id)
}

func (h *AccountHandler) DeletePaymentMethod(ctx restate.Context, params DeletePaymentMethodParams) error {
    // Verify ownership, delete from DB
}

func (h *AccountHandler) TokenizeCard(ctx restate.Context, params TokenizeCardParams) (payment.TokenizeResult, error) {
    // Find card provider from paymentMap, call Tokenize()
}
```

Note: `AccountHandler` needs access to the payment map. Pass it via constructor or look it up from the order module. The simplest approach is to accept the card provider directly in the constructor.

- [ ] **Step 3: Create HTTP handler**

Create `internal/module/account/transport/echo/payment_method.go`:

```go
package accountecho

// Handler methods for:
// GET  /api/v1/account/payment-method          → ListPaymentMethods
// POST /api/v1/account/payment-method          → CreatePaymentMethod
// PUT  /api/v1/account/payment-method/:id/default → SetDefaultPaymentMethod
// DELETE /api/v1/account/payment-method/:id     → DeletePaymentMethod
// POST /api/v1/account/payment-method/tokenize  → TokenizeCard
```

- [ ] **Step 4: Register routes in account handler**

In the account transport `NewHandler` function, add the payment method routes.

- [ ] **Step 5: Regenerate Restate proxy**

```bash
go generate ./internal/module/account/biz/
```

- [ ] **Step 6: Verify build**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/module/account/
git commit -m "add payment method biz and transport endpoints"
```

---

### Task 8: Frontend — Update Types and Hooks

**Files:**
- Modify: `core/account/payment-method.ts` (in shopnexus-website-v3)
- Modify: `core/order/order.buyer.ts` (in shopnexus-website-v3)

- [ ] **Step 1: Update `PaymentMethod` type**

In `core/account/payment-method.ts`, update the type:

```ts
export type PaymentMethod = {
    id: string
    account_id: string
    type: "card" | "ewallet" | "bank"
    provider: string
    label: string
    data: {
        token?: string
        brand?: string
        last4?: string
        exp_month?: number
        exp_year?: number
        card_type?: "credit" | "debit"
    }
    is_default: boolean
    date_created: string
    date_updated: string
}
```

- [ ] **Step 2: Add `useTokenizeCard` hook**

```ts
export function useTokenizeCard() {
    return useMutation({
        mutationFn: async (params: { return_url?: string }) => {
            return customFetch<{ form_url?: string; client_config?: Record<string, unknown> }>(
                "account/payment-method/tokenize",
                { method: "POST", body: JSON.stringify(params) }
            )
        },
    })
}
```

- [ ] **Step 3: Update `TPayment` type**

In `core/order/order.buyer.ts`, add `payment_method_id`:

```ts
export type TPayment = {
    id: number
    account_id: string
    option: string
    payment_method_id?: string
    status: string
    amount: number
    data: unknown
    date_created: string
    date_paid?: string
    date_expired: string
}
```

- [ ] **Step 4: Commit**

```bash
cd ../shopnexus-website-v3
git add core/account/payment-method.ts core/order/order.buyer.ts
git commit -m "update payment method types and add tokenize hook"
```

---

### Task 9: Frontend — Payment Method Selector Component

**Files:**
- Modify: `app/(marketplace)/account/orders/page.tsx` (in shopnexus-website-v3)
- Modify: `app/(marketplace)/account/orders/[id]/page.tsx` (in shopnexus-website-v3)

- [ ] **Step 1: Create payment method selector in the pay dialog**

In both order pages, replace the hardcoded `payment_option: "default"` with a selector that:

1. Fetches `useListPaymentMethods()` and `useListServiceOption({ category: "payment" })`
2. Shows saved cards at the top (with brand icon, `****last4`, default badge)
3. Shows redirect providers below (VNPay, SePay, COD)
4. Shows "Add new card" button at the bottom
5. Pre-selects the default card if one exists
6. Sends `pm:<id>` for saved cards or the option slug for redirect providers

The pay dialog already exists in these files — it calls `payMutation.mutateAsync({ order_ids, payment_option })`. Change `payment_option` from `"default"` to the selected value.

- [ ] **Step 2: Handle redirect vs instant result**

The existing code already handles `result.url` (redirects) vs no URL (toast success). Card payments will return no URL — the existing logic works as-is.

- [ ] **Step 3: Commit**

```bash
git add app/\(marketplace\)/account/orders/
git commit -m "add payment method selector to pay dialog"
```

---

### Task 10: Frontend — Redesign Account Payment Methods Page

**Files:**
- Modify: `app/(marketplace)/account/payment/page.tsx` (in shopnexus-website-v3)

- [ ] **Step 1: Redesign the page**

Replace the current implementation with:

1. **Card list** — each card shows: brand icon (visa/mastercard/jcb), `**** **** **** {last4}`, expiry `{exp_month}/{exp_year}`, card type badge (credit/debit), default badge
2. **"Add Card" button** — calls `useTokenizeCard()`, then either redirects to `form_url` or shows the processor's JS SDK form (based on `client_config`)
3. **Card actions** — set as default (`useSetDefaultPaymentMethod`), delete (`useDeletePaymentMethod`)
4. Remove `e-wallet` and `bank-transfer` from the UI (keep them in the type definition for future)
5. Remove the freeform JSON data textarea

- [ ] **Step 2: Commit**

```bash
git add app/\(marketplace\)/account/payment/
git commit -m "redesign account payment methods page for card display"
```

---

### Task 11: Final Verification

- [ ] **Step 1: Build BE**

```bash
cd ../shopnexus-server
go build ./...
```

- [ ] **Step 2: Build FE**

```bash
cd ../shopnexus-website-v3
bun run build
```

- [ ] **Step 3: Verify payment map registration**

Start the server, check logs for registered payment options. Card provider should appear only when `cardPayment.provider` is configured.

- [ ] **Step 4: Final commit if needed**

```bash
git add -A
git commit -m "final cleanup for payment method refactor"
```
