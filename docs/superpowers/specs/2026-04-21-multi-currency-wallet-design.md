# Multi-Currency Wallet via Country-Inferred Currency

## Summary

Extend the platform from single-currency wallet semantics to true multi-currency by deriving each user's wallet currency from `profile.country` at runtime, without adding a `currency` column to `account.wallet`. Each user has exactly one wallet in exactly one currency (determined by their country). Cross-currency checkout is supported via FX snapshot into `payment.exchange_rate`. No multi-balance wallets, no self-serve FX conversion, no seller prices in foreign currency.

## Motivation

The platform already has multi-currency display (via `preferred_currency` + `Intl.NumberFormat`) and a schema that anticipates cross-currency settlement (`payment.buyer_currency`, `payment.seller_currency`, `payment.exchange_rate`). But the wallet is single-balance with an implicit currency, which breaks when a non-VND user refunds a VND order (or vice versa). Prior discussions considered removing the wallet entirely, but the wallet is needed for seller escrow/payout and buyer refund destination in a marketplace.

A full multi-balance wallet (Wise/Revolut style) requires an FX license in Vietnam (Pháp lệnh ngoại hối), which the platform does not hold. A simpler "Shopee-per-user" model — one wallet per user, currency fixed by country — avoids this while still enabling cross-border checkout at the gateway/FX layer.

## Builds On

- `2026-04-18-currency-refactor-design.md` — currency as first-class field.
- `2026-04-20-preferred-currency-design.md` — exchange rate service, display-layer conversion, `profile.preferred_currency`.
- Subsequent commits migrating exchange rates to cache (`currencyapi` provider) and hardcoded decimal maps to CLDR via `golang.org/x/text/currency`.

## Scope

In:
- Add `profile.country` (ISO 3166-1 alpha-2), required at signup.
- Wallet currency = `currency.FromRegion(profile.country)`, derived at query time.
- Enforce `spu.currency == InferCurrency(seller.profile.country)` at product creation.
- Cross-currency checkout: debit buyer wallet in buyer currency, credit seller wallet in seller currency, FX snapshot to `payment.exchange_rate`.
- Refund flow uses the original `payment.exchange_rate` snapshot (not current rate).
- Country change endpoint with balance-zero guard.

Out:
- Multi-balance wallet per user.
- Self-serve wallet-to-wallet FX conversion.
- FX spread (MVP uses mid-market rate; platform eats cost).
- Admin override for users in countries with ambiguous currency (Cambodia KHR vs USD, etc.).
- Seller pricing in a currency different from their wallet currency.
- Explicit escrow table (existing implicit flow via order state is reused).

## Design

### 1. Schema changes

Dev stage — edit init migrations in place, no new migration file.

`internal/module/account/db/migrations/0001_init.up.sql`:

```diff
 CREATE TABLE IF NOT EXISTS "account"."profile" (
     "account_id" UUID NOT NULL,
     ...
+    -- ISO 3166-1 alpha-2. Drives wallet currency via x/text/currency.FromRegion.
+    "country" VARCHAR(2) NOT NULL,
     "preferred_currency" VARCHAR(3) NOT NULL,
     ...
+    CONSTRAINT "profile_country_format" CHECK ("country" ~ '^[A-Z]{2}$'),
     ...
 );
```

`0001_init.down.sql`: mirror the drop.

No change to `account.wallet`. No change to `order.payment`.

### 2. Currency inference helper

New shared package `internal/shared/currency/`:

```go
// internal/shared/currency/infer.go
package currency

import (
    "fmt"

    "golang.org/x/text/currency"
    "golang.org/x/text/language"
)

var ErrNoCurrencyForRegion = fmt.Errorf("no canonical currency for region")

// Infer returns the canonical ISO 4217 code for an ISO 3166-1 alpha-2 country code.
// Eurozone countries all return EUR. Returns ErrNoCurrencyForRegion for regions
// without a canonical currency (e.g. Antarctica).
func Infer(countryCode string) (string, error) {
    region, err := language.ParseRegion(countryCode)
    if err != nil {
        return "", fmt.Errorf("parse region %q: %w", countryCode, err)
    }
    unit, ok := currency.FromRegion(region)
    if !ok {
        return "", fmt.Errorf("%w: %s", ErrNoCurrencyForRegion, countryCode)
    }
    return unit.String(), nil
}
```

Callers: signup wallet default, checkout (buyer/seller currency lookup), product creation validation, any endpoint returning wallet balance with currency label.

### 3. Signup & country change

**Signup (extend existing):**
- Add required `country` field to signup request DTO.
- Validate via `Infer(country)` at request time — reject early if no currency mapping.
- On account creation, wallet row implicitly has currency = `Infer(country)`; nothing to persist in wallet table.
- `profile.preferred_currency` defaults to `Infer(country)` if client doesn't specify.

**Change country (new endpoint):**

`PATCH /api/v1/account/profile/country`

```go
type UpdateCountryParams struct {
    Country string `json:"country" validate:"required,iso3166_1_alpha2"`
}
```

Biz logic (`internal/module/account/biz/profile.go`):

```go
func (b *AccountHandler) UpdateCountry(ctx restate.Context, params UpdateCountryParams) error {
    // Validate currency mapping exists for the new country.
    if _, err := sharedcurrency.Infer(params.Country); err != nil {
        return sharedmodel.WrapErr("validate country", err)
    }
    // Guard: wallet must be empty to change currency.
    balance, err := b.GetWalletBalance(ctx, callerAccountID)
    if err != nil { return err }
    if balance != 0 {
        return sharedmodel.ErrConflict("wallet_not_empty",
            "withdraw or spend wallet balance before changing country")
    }
    return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
        return b.storage.Querier().UpdateProfileCountry(ctx, ...)
    })
}
```

**Change preferred_currency (existing endpoint):** unchanged, no balance guard needed — display-only.

### 4. Catalog: enforce seller pricing currency

`internal/module/catalog/biz/spu.go` — in product create/update path:

```go
sellerCurrency, err := sharedcurrency.Infer(seller.Profile.Country)
if err != nil { return err }
if params.Currency != sellerCurrency {
    return sharedmodel.ErrValidation("currency_mismatch",
        fmt.Sprintf("seller in %s must price products in %s, got %s",
            seller.Profile.Country, sellerCurrency, params.Currency))
}
```

Consequence: every order item has `sellerCurrency == spu.currency == Infer(seller.country)`. Simplifies downstream FX math to a single conversion (buyer → seller), never two.

### 5. Checkout flow

Changes concentrated in `internal/module/order/biz/checkout.go::BuyerCheckout`:

```go
// After loading buyer profile and product/seller info:
buyerCurrency, _ := sharedcurrency.Infer(buyer.Profile.Country)
sellerCurrency := spu.Currency  // guaranteed == Infer(seller.country)

var rate float64 = 1.0
if buyerCurrency != sellerCurrency {
    rate, err = b.exchange.GetRate(ctx, sellerCurrency, buyerCurrency)
    if err != nil {
        return zero, sharedmodel.WrapErr("fx rate lookup", err)
    }
}

// productTotal is in sellerCurrency smallest unit (from spu/sku).
// buyerTotal = what to debit from buyer wallet (in buyerCurrency).
buyerTotal := int64(math.Round(float64(productTotal) * rate))

// Snapshot into payment row — already-existing columns.
paymentParams := orderdb.CreatePaymentParams{
    Amount:         buyerTotal,
    BuyerCurrency:  buyerCurrency,
    SellerCurrency: sellerCurrency,
    ExchangeRate:   decimal.NewFromFloat(rate),
    ...
}
```

**Wallet debit path** (inside `BuyerCheckout`, when `UseWallet=true`): debit `buyerTotal` from buyer's wallet. Wallet queries are currency-agnostic (amounts are opaque int64s); the caller owns currency semantics via profile lookup.

**Seller credit path:** currently the wallet has no seller-credit flow — only `Payment`/`Refund`/`TopUp` transaction types exist, all buyer-side. Seller payout on delivery confirmation is out of scope for this spec (separate design). What this spec guarantees for sellers is: **when a cross-currency refund happens, the seller is debited in their own currency using the snapshot rate**, even if seller-credit was never implemented via wallet. That debit creates a seller wallet row with negative balance if needed; payout engineering can reconcile later. Flag for review: whether negative seller balance is acceptable as an MVP placeholder, or whether we need the seller-credit side first.

**Spread policy:** MVP uses mid-market rate from cache (`exchange.GetRate`). Platform absorbs any FX movement during the settlement window. `TODO` comment in `checkout.go` flagging this for post-launch revisit.

### 6. Refund flow

Changes in `refund.go`, `timeout.go::CancelUnpaidCheckout`, `timeout.go::AutoCancelPendingItems`, `checkout.go::CancelBuyerPending`, and `incoming.go` refund branch.

All refund sites already compute `totalRefund` in buyer currency (paid amounts are in buyer currency on `order.item.paid_amount`). Required changes:

1. **Credit buyer wallet** (already happens): `WalletCredit(buyerID, totalRefund)`. No change — amount is already in buyer currency.
2. **Debit seller wallet** (new, but see Section 5 caveat): compute `sellerRefund = round(totalRefund / payment.exchange_rate)` and call `WalletDebit(sellerID, sellerRefund)`. Uses a new wallet transaction type `Payout` (or `SellerDebit`) distinct from buyer `Payment`. Seller wallet may go negative if seller-credit flow isn't yet implemented — acceptable for MVP pending separate payout spec.
3. **Rate source:** always `payment.exchange_rate` (snapshot from checkout), never `exchange.GetRate(now)`. Prevents FX rate movement from distorting the refund.
4. **Rounding:** integer smallest-unit math. Use `math.Round(float64(amount) / rate)` at boundary; document that buyer-side number is authoritative (1 unit rounding loss absorbed by platform).

### 7. VN domestic regulatory guard

No explicit code check required. The invariants produce correct behavior:

- VN buyer → buyerCurrency = VND.
- VN seller → all their SPUs have currency = VND.
- Same-currency path: `rate = 1.0`, no FX event, transaction is in VND — compliant with Pháp lệnh ngoại hối.
- Cross-border (VN buyer + foreign seller, or vice versa): FX event occurs, which is legal for import/export.

Only way to violate invariant: admin manually edits `spu.currency` to diverge from seller country. Out of scope — admin tools not exposed in MVP.

### 8. Transport (HTTP) layer

New endpoint:

```
PATCH /api/v1/account/profile/country
  body: { "country": "US" }
  200: { "country": "US", "inferred_currency": "USD" }
  409: { "code": "wallet_not_empty", "balance": 12345 }
```

Signup endpoint: extend body validator to require `country`. Existing profile GET should include `country` and derived `inferred_currency` in response.

### 9. Frontend impact (sketch)

- **Signup form:** country dropdown. Suggested client-side list from `Intl.DisplayNames(..., {type:'region'})`. Required field.
- **Profile settings:** display country + preferred_currency as separate controls. Country editor calls PATCH with balance guard; show tooltip explaining the constraint.
- **Product creation (seller):** currency is read-only, auto-filled from seller's inferred wallet currency. Validation prevents submitting other currencies.
- **Checkout:** if cross-currency, show buyer "You pay X in YOUR_CURRENCY (≈ Y in SELLER_CURRENCY at rate Z)" before confirmation. Rate snapshot is taken at checkout confirm, not cart.

## Files Affected

### Backend (new)
- `internal/shared/currency/infer.go`
- `internal/shared/currency/infer_test.go`

### Backend (modified)
- `internal/module/account/db/migrations/0001_init.up.sql` — add `country` column + CHECK.
- `internal/module/account/db/migrations/0001_init.down.sql` — mirror drop.
- `internal/module/account/db/queries/profile_custom.sql` — add `UpdateProfileCountry` query.
- `internal/module/account/db/queries/generated_queries.sql` — regenerate via `make pgtempl`.
- `internal/module/account/model/*` — add `Country` field to Profile model.
- `internal/module/account/biz/profile.go` — add `UpdateCountry` method.
- `internal/module/account/biz/signup.go` (or equivalent) — accept country in signup DTO.
- `internal/module/account/transport/echo/profile.go` — PATCH country endpoint.
- `internal/module/catalog/biz/spu.go` — enforce `spu.currency == Infer(seller.country)`.
- `internal/module/order/biz/checkout.go::BuyerCheckout` — cross-currency FX logic.
- `internal/module/order/biz/refund.go` — debit seller wallet using snapshot rate.
- `internal/module/order/biz/timeout.go` — same snapshot rate usage in both timeout paths.
- `internal/module/order/biz/incoming.go` — same for refund on confirmation failure.
- SQLC regenerate everywhere above after query changes.

### Frontend (`../shopnexus-website-v3/`)
- Signup form — country select input.
- Profile settings page — country edit control with 409 error UX.
- Seller product creation — currency field becomes read-only, derived from seller profile.
- Checkout page — FX preview block before confirm.
- Any place currently reading `preferred_currency` — no change required.

## Rollout

1. Backend first: schema + helper + endpoint + biz changes. Ship behind implicit rollout (dev-only, no prod users).
2. Seed/reset dev accounts via existing seed scripts to populate `country = 'VN'`.
3. Frontend signup flow updated to require country.
4. Frontend checkout FX preview added.
5. E2E smoke: VN buyer + VN seller (same-currency), VN buyer + US seller (cross-currency wallet debit), refund for cross-currency order uses snapshot rate.

## Testing Strategy

Backend unit:
- `sharedcurrency.Infer`: VN→VND, DE→EUR, FR→EUR, US→USD, AQ (Antarctica)→error.
- `UpdateCountry`: balance=0 success, balance>0 returns 409, invalid country returns validation error.
- Checkout same-currency: rate=1, no exchange lookup invoked.
- Checkout cross-currency: rate snapshot written to `payment.exchange_rate`, buyer/seller amounts differ by rate.
- Refund uses `payment.exchange_rate` even if `exchange.GetRate(now)` would return different value.

Backend integration:
- Full checkout saga with cross-currency buyer/seller, verify seller wallet credit matches product price (not buyer amount).
- Cancel-unpaid-checkout: buyer gets back exactly what they paid, seller is not debited.
- Cancel-after-paid (timeout 48h): buyer gets back exactly what they paid, seller is debited exactly what they were credited.

## Open Questions

Resolved during brainstorm:
- Wallet currency source: country-inferred at runtime.
- Regulatory: handled by invariants, no explicit guard code.
- Spread: MVP none, revisit post-launch.
- Admin override for ambiguous-currency countries: deferred, YAGNI.
- Country-change UX with non-zero balance: hard block, user must withdraw/spend first.

Flagged for user confirmation before implementation:
- **Seller-credit flow is not currently wired into the wallet.** This spec assumes refund debits to seller can go negative as an MVP placeholder. Alternative: block implementation until seller-credit/payout flow is specced in a separate design. Decision needed before executing the plan.
- **Wallet transaction types:** need to add `Payout`/`SellerDebit` to `account_wallet_transaction_type` enum, or overload existing `Payment` type with a note distinguishing buyer vs seller. Prefer new enum value for clarity.
