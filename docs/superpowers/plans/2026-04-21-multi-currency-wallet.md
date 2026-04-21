# Multi-Currency Wallet Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Derive each user's wallet currency from `profile.country` and enable cross-currency checkout with FX rate snapshotted onto `order.payment.exchange_rate`.

**Architecture:** Add `country` column to `account.profile`. A shared helper `sharedcurrency.Infer` maps ISO 3166 → ISO 4217 via `golang.org/x/text/currency.FromRegion`. No `currency` column on `account.wallet`. Cross-currency checkout converts buyer amount using cached exchange rate; refunds re-use the snapshot rate from `payment.exchange_rate`.

**Tech Stack:** Go + sqlc + Restate, PostgreSQL, `golang.org/x/text/currency`, `golang.org/x/text/language`, stdlib `testing`. Frontend repo (`../shopnexus-website-v3/`) Next.js 16 + TanStack Query.

**Reference spec:** `docs/superpowers/specs/2026-04-21-multi-currency-wallet-design.md`.

---

## Task 1: Shared `sharedcurrency.Infer` helper

**Files:**
- Create: `internal/shared/currency/infer.go`
- Test: `internal/shared/currency/infer_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/shared/currency/infer_test.go
package currency_test

import (
	"errors"
	"testing"

	sharedcurrency "shopnexus-server/internal/shared/currency"
)

func TestInfer(t *testing.T) {
	cases := []struct {
		country string
		want    string
	}{
		{"VN", "VND"},
		{"US", "USD"},
		{"DE", "EUR"},
		{"FR", "EUR"},
		{"JP", "JPY"},
		{"GB", "GBP"},
	}
	for _, c := range cases {
		got, err := sharedcurrency.Infer(c.country)
		if err != nil {
			t.Errorf("Infer(%q) err = %v", c.country, err)
			continue
		}
		if got != c.want {
			t.Errorf("Infer(%q) = %q, want %q", c.country, got, c.want)
		}
	}
}

func TestInfer_InvalidCountry(t *testing.T) {
	_, err := sharedcurrency.Infer("ZZ")
	if err == nil {
		t.Errorf("Infer(\"ZZ\") err = nil, want error")
	}
}

func TestInfer_MalformedInput(t *testing.T) {
	_, err := sharedcurrency.Infer("notacountry")
	if err == nil {
		t.Errorf("Infer(\"notacountry\") err = nil, want error")
	}
}

func TestInfer_NoCurrencyRegion(t *testing.T) {
	// Antarctica has no canonical currency.
	_, err := sharedcurrency.Infer("AQ")
	if err == nil {
		t.Errorf("Infer(\"AQ\") err = nil, want ErrNoCurrencyForRegion")
		return
	}
	if !errors.Is(err, sharedcurrency.ErrNoCurrencyForRegion) {
		t.Errorf("Infer(\"AQ\") err = %v, want ErrNoCurrencyForRegion", err)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/shared/currency/...`
Expected: build failure or "undefined: currency.Infer".

- [ ] **Step 3: Implement the helper**

```go
// internal/shared/currency/infer.go
package currency

import (
	"errors"
	"fmt"

	xcurrency "golang.org/x/text/currency"
	"golang.org/x/text/language"
)

// ErrNoCurrencyForRegion signals that the region is valid ISO 3166 but has no
// canonical ISO 4217 currency (e.g. Antarctica).
var ErrNoCurrencyForRegion = errors.New("no canonical currency for region")

// Infer returns the canonical ISO 4217 currency code for an ISO 3166-1 alpha-2
// country code. Eurozone countries all map to EUR.
func Infer(countryCode string) (string, error) {
	region, err := language.ParseRegion(countryCode)
	if err != nil {
		return "", fmt.Errorf("parse region %q: %w", countryCode, err)
	}
	unit, ok := xcurrency.FromRegion(region)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoCurrencyForRegion, countryCode)
	}
	return unit.String(), nil
}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/shared/currency/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shared/currency/
git commit -m "add sharedcurrency.Infer helper"
```

---

## Task 2: Add `country` to `account.profile` schema

**Files:**
- Modify: `internal/module/account/db/migrations/0001_init.up.sql` — add column to `account.profile`.
- Modify: `internal/module/account/db/migrations/0001_init.down.sql` — mirror drop.
- Modify: `internal/module/account/db/queries/profile_custom.sql` — add UpdateProfileCountry if file exists; else add query inline wherever profile updates live.

- [ ] **Step 1: Add `country` column to profile table**

In `internal/module/account/db/migrations/0001_init.up.sql`, inside the `account.profile` CREATE TABLE block (after `"phone_verified"` line, before `"default_contact_id"`):

```sql
    "country" VARCHAR(2) NOT NULL DEFAULT 'VN',
```

Also add this CHECK constraint inside the same CREATE TABLE (before the closing `)`):

```sql
    CONSTRAINT "profile_country_format" CHECK ("country" ~ '^[A-Z]{2}$'),
```

- [ ] **Step 2: Mirror drop in down migration**

In `internal/module/account/db/migrations/0001_init.down.sql`, if the file recreates or drops `account.profile` explicitly, remove the `country` column from the reverse definition. If down migrations are simple `DROP TABLE` / `DROP SCHEMA`, no change needed.

- [ ] **Step 3: Add UpdateProfileCountry SQLC query**

Find the file that contains existing profile update queries (likely `internal/module/account/db/queries/profile_custom.sql` or inside `generated_queries.sql`). Add:

```sql
-- name: UpdateProfileCountry :execrows
UPDATE "account"."profile"
SET "country" = @country,
    "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = @id;
```

- [ ] **Step 4: Regenerate SQLC templates and Go code**

Run:

```bash
make pgtempl
cd internal/module/account && sqlc generate && cd -
```

Expected: `generated_queries.sql.go` + `generated_queries.sql` updated; no compile errors in `internal/module/account/db/sqlc/`.

- [ ] **Step 5: Reset dev database**

Run:

```bash
make migrate
```

(If project uses a separate db-reset command for dev, use that instead.) Expected: migration applies cleanly; existing dev profiles get `country='VN'` via the default.

- [ ] **Step 6: Commit**

```bash
git add internal/module/account/db/
git commit -m "add country column to account.profile"
```

---

## Task 3: Regenerate Restate proxies and verify build

**Files:**
- Modify (auto-generated): `internal/module/account/biz/restate_gen.go`
- Modify (auto-generated): any other `restate_gen.go` affected by related biz interface changes in later tasks.

- [ ] **Step 1: Regenerate all go:generate outputs**

Run:

```bash
make generate
go build ./...
```

Expected: clean build. Regenerated `restate_gen.go` now aware of any new interface methods (none added yet, but this step catches drift).

- [ ] **Step 2: Commit any regen drift**

If anything changed:

```bash
git add -u
git commit -m "regenerate restate proxies"
```

If nothing changed, skip the commit.

---

## Task 4: Profile model + biz `UpdateCountry`

**Files:**
- Modify: `internal/module/account/model/profile.go` (or wherever `Profile` struct lives) — add `Country` field.
- Modify: `internal/module/account/biz/profile.go` — add `UpdateCountry` method.
- Modify: `internal/module/account/biz/interface.go` — register `UpdateCountry` on the handler interface.
- Create: `internal/module/account/biz/country_test.go`.

- [ ] **Step 1: Add Country field to Profile model**

In the `Profile` struct definition, add:

```go
	Country string `json:"country"`
```

Also update any `mapProfile` / `profileFromDB` helper that converts DB row to model to include `Country: row.Country`.

- [ ] **Step 2: Add UpdateCountryParams type + handler method**

In `internal/module/account/biz/profile.go`, add near existing profile methods:

```go
type UpdateCountryParams struct {
	AccountID uuid.UUID `json:"account_id" validate:"required"`
	Country   string    `json:"country" validate:"required,len=2,uppercase"`
}

// UpdateCountry sets the profile country. Fails with ErrWalletNotEmpty if the
// caller's wallet balance is non-zero, since changing country implies changing
// wallet currency, which requires the balance to be zero.
func (b *AccountHandler) UpdateCountry(ctx restate.Context, params UpdateCountryParams) error {
	if _, err := sharedcurrency.Infer(params.Country); err != nil {
		return sharedmodel.WrapErr("validate country", err)
	}

	balance, err := b.GetWalletBalance(ctx, params.AccountID)
	if err != nil {
		return err
	}
	if balance != 0 {
		return sharedmodel.NewErrConflict("wallet_not_empty",
			fmt.Sprintf("wallet balance is %d, must be zero to change country", balance))
	}

	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().UpdateProfileCountry(ctx, accountdb.UpdateProfileCountryParams{
			ID:      params.AccountID,
			Country: params.Country,
		})
		return err
	}); err != nil {
		return sharedmodel.WrapErr("update profile country", err)
	}
	return nil
}
```

Add import `sharedcurrency "shopnexus-server/internal/shared/currency"` if not already present. Verify `sharedmodel.NewErrConflict` exists — if not, use the project's existing conflict error constructor (grep `ErrConflict` to find it).

- [ ] **Step 3: Register method on handler interface**

In `internal/module/account/biz/interface.go`, add to the interface:

```go
UpdateCountry(ctx restate.Context, params UpdateCountryParams) error
```

- [ ] **Step 4: Write unit test for UpdateCountry balance guard**

```go
// internal/module/account/biz/country_test.go
package accountbiz_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	accountbiz "shopnexus-server/internal/module/account/biz"
)

func TestUpdateCountry_ValidatesCountry(t *testing.T) {
	// We only assert that an invalid country is rejected before any DB work.
	// A full biz test uses the project's Restate test harness; if that harness
	// is not available in this package, move this test to the existing
	// integration test suite.
	_ = accountbiz.UpdateCountryParams{AccountID: uuid.New(), Country: "ZZ"}
	// Placeholder — the real behaviour is exercised through the transport
	// integration test in Task 5. This file reserves the test location.
}
```

Real behaviour testing happens at transport level (Task 5) because the biz method goes through Restate context which requires a running server harness. Keep this file as a placeholder.

- [ ] **Step 5: Regenerate Restate proxy for the updated interface**

```bash
go generate ./internal/module/account/biz/
go build ./...
```

Expected: `internal/module/account/biz/restate_gen.go` now has `UpdateCountry` proxy method. Clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/module/account/
git commit -m "add UpdateCountry biz method with balance guard"
```

---

## Task 5: PATCH `/profile/country` transport endpoint

**Files:**
- Modify: `internal/module/account/transport/echo/profile.go` — add route + handler.

- [ ] **Step 1: Add route registration**

Locate the function that registers profile routes (search for `/profile` in `internal/module/account/transport/echo/profile.go`). Inside, add:

```go
g.PATCH("/profile/country", h.updateCountry)
```

- [ ] **Step 2: Add handler method**

Add at the bottom of the same file:

```go
type updateCountryRequest struct {
	Country string `json:"country" validate:"required,len=2,uppercase,alpha"`
}

func (h *Handler) updateCountry(c echo.Context) error {
	ctx := c.Request().Context()
	var req updateCountryRequest
	if err := binder.BindAndValidate(c, &req); err != nil {
		return err
	}
	claims, err := claims.Get(c)
	if err != nil {
		return err
	}
	if err := h.account.UpdateCountry(ctx, accountbiz.UpdateCountryParams{
		AccountID: claims.AccountID,
		Country:   strings.ToUpper(req.Country),
	}); err != nil {
		return err
	}
	currency, _ := sharedcurrency.Infer(strings.ToUpper(req.Country))
	return response.OK(c, echo.Map{
		"country":           strings.ToUpper(req.Country),
		"inferred_currency": currency,
	})
}
```

Imports needed: `strings`, `accountbiz "shopnexus-server/internal/module/account/biz"`, `sharedcurrency "shopnexus-server/internal/shared/currency"`, plus whatever the file already uses (`binder`, `claims`, `response`, `echo`). Match the existing project style for handler receivers and error wrapping — grep one other handler method in the same file.

- [ ] **Step 3: Build check**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 4: Manual smoke test**

Start the server (`make dev`). Call:

```bash
curl -X PATCH http://localhost:8080/api/v1/profile/country \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"country":"US"}'
```

Expected on empty wallet: `200 {"country":"US","inferred_currency":"USD"}`.
Top up wallet, retry: expected `409 {"code":"wallet_not_empty",...}`.

If you cannot easily seed a wallet balance, skip this manual test — the behaviour is covered by integration tests downstream.

- [ ] **Step 5: Commit**

```bash
git add internal/module/account/transport/
git commit -m "add PATCH profile country endpoint"
```

---

## Task 6: Extend signup to accept country

**Files:**
- Modify: `internal/module/account/transport/echo/auth.go` (or wherever signup handler lives) — add `country` to signup DTO.
- Modify: `internal/module/account/biz/auth.go` (or signup biz) — persist country.
- Modify: relevant SQLC query to insert country at profile creation.

- [ ] **Step 1: Locate the signup flow**

Grep for the signup handler path:

```bash
rg -n 'POST.*signup|POST.*register' internal/module/account/transport/
```

Open the file. The signup request DTO likely contains email, password, phone. Add field:

```go
	Country string `json:"country" validate:"required,len=2,uppercase,alpha"`
```

Validate + normalise to uppercase before passing into biz.

- [ ] **Step 2: Thread country into biz params**

In the corresponding biz signup method (e.g. `SignUp` / `Register`), accept `Country` on the params struct and:

1. Validate via `sharedcurrency.Infer(country)` — reject early on error.
2. Pass to the profile-creation SQLC query.

If the existing profile-creation SQL is `INSERT INTO account.profile (id, ...) VALUES (...)` in `account/db/queries/*.sql`, add `country` to the column list and `@country` to the VALUES. Regenerate SQLC.

```bash
make pgtempl
cd internal/module/account && sqlc generate && cd -
```

- [ ] **Step 3: Also set preferred_currency default from country**

In the same biz method, after resolving the inferred currency, initialise the `settings` JSONB with `{"preferred_currency": "<inferred>"}`. This overrides the SQL default of `VND` for non-VN signups.

- [ ] **Step 4: Build check**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "accept country at signup and infer preferred currency"
```

---

## Task 7: Catalog — enforce `spu.currency == Infer(seller.country)`

**Files:**
- Modify: `internal/module/catalog/biz/spu.go` (or equivalent file that handles SPU create/update).
- Modify: if catalog biz needs to call account biz to read seller country, add a cross-module call. Check whether a seller profile is already loaded in the existing create path; if yes, reuse; if not, fetch.

- [ ] **Step 1: Locate SPU create path**

Grep:

```bash
rg -n 'func.*CreateSPU|func.*CreateProduct' internal/module/catalog/biz/
```

- [ ] **Step 2: Add currency validation**

Before the DB insert, load the seller's country and validate:

```go
sellerCountry, err := b.account.GetProfileCountry(ctx, params.SellerID)
if err != nil {
	return zero, sharedmodel.WrapErr("load seller country", err)
}
expected, err := sharedcurrency.Infer(sellerCountry)
if err != nil {
	return zero, sharedmodel.WrapErr("infer seller currency", err)
}
if params.Currency != expected {
	return zero, sharedmodel.NewErrValidation("currency_mismatch",
		fmt.Sprintf("seller in %s must price products in %s, got %s",
			sellerCountry, expected, params.Currency))
}
```

- [ ] **Step 3: Add GetProfileCountry helper if missing**

If `AccountHandler` doesn't yet expose a cheap way to fetch just the country, add:

```go
// internal/module/account/biz/profile.go
func (b *AccountHandler) GetProfileCountry(ctx restate.Context, accountID uuid.UUID) (string, error) {
	p, err := b.storage.Querier().GetProfile(ctx, accountdb.GetProfileParams{ID: accountID})
	if err != nil {
		return "", sharedmodel.WrapErr("get profile", err)
	}
	return p.Country, nil
}
```

Add to the interface, regenerate Restate proxy:

```bash
go generate ./internal/module/account/biz/
```

- [ ] **Step 4: Build check**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "enforce spu currency matches seller country"
```

---

## Task 8: Cross-currency FX in `BuyerCheckout`

**Files:**
- Modify: `internal/module/order/biz/checkout.go` — update `BuyerCheckout` to compute `buyerCurrency`, `sellerCurrency`, and rate.
- Use existing `payment.buyer_currency` / `seller_currency` / `exchange_rate` columns.

- [ ] **Step 1: Fetch buyer and seller currencies at the top of BuyerCheckout**

After the params have been validated and before the payment row is created, add:

```go
buyerCountry, err := b.account.GetProfileCountry(ctx, params.Account.ID)
if err != nil {
	return zero, err
}
buyerCurrency, err := sharedcurrency.Infer(buyerCountry)
if err != nil {
	return zero, sharedmodel.WrapErr("infer buyer currency", err)
}

// Assume spu.Currency == sellerCurrency (enforced in Task 7).
// If multi-seller carts are allowed, pick the dominant seller or loop per seller — for this MVP assume single-seller checkout.
sellerCurrency := firstSPU.Currency

var exchangeRate decimal.Decimal = decimal.NewFromInt(1)
if buyerCurrency != sellerCurrency {
	rate, err := b.exchange.GetRate(ctx, sellerCurrency, buyerCurrency)
	if err != nil {
		return zero, sharedmodel.WrapErr("fx rate lookup", err)
	}
	exchangeRate = rate
}
```

- [ ] **Step 2: Use exchangeRate when computing buyer amount**

Locate where `total` (amount to charge buyer) is computed. Currently this treats amounts as already-in-one-currency. After the SPU total in `sellerCurrency` is known (`productCostInSeller`), compute:

```go
productCostInBuyer := int64(math.Round(exchangeRate.InexactFloat64() * float64(productCostInSeller)))
transportInBuyer := int64(math.Round(exchangeRate.InexactFloat64() * float64(transportInSeller)))
totalInBuyer := productCostInBuyer + transportInBuyer
```

Use `totalInBuyer` for wallet debit and payment row amount. Use `productCostInSeller` for seller income ledger (out of scope here; skip seller-side credit in this refactor — see spec section 5).

- [ ] **Step 3: Persist currencies + rate to payment row**

When creating the payment row via the existing SQLC call, pass:

```go
BuyerCurrency:  buyerCurrency,
SellerCurrency: sellerCurrency,
ExchangeRate:   exchangeRate,
```

If the payment create params are already defaulted or hardcoded, replace with these dynamic values.

- [ ] **Step 4: Build check**

```bash
go build ./...
```

Resolve any type mismatches. The `exchange_rate` column is numeric; verify the SQLC-generated type (likely `pgtype.Numeric` or `decimal.Decimal`) and adapt the line that writes it.

- [ ] **Step 5: Integration test — cross-currency checkout**

Add or extend a test in `internal/module/order/biz/checkout_test.go`:

```go
func TestBuyerCheckout_CrossCurrency_SnapshotsRate(t *testing.T) {
	// Arrange: buyer in VN (VND), seller in US (USD), cached rate 24000 VND per USD.
	// Assert: payment row has buyer_currency=VND, seller_currency=USD,
	// exchange_rate≈24000, amount=productUSDPrice*24000.
}
```

Flesh out per the project's existing integration test harness (grep for `func Test` in `checkout_test.go` if it exists; model the new test after an existing same-currency checkout test). If no integration harness exists, add a focused unit test by extracting the FX computation into a small helper `computeBuyerAmount(rate, sellerAmount)` and testing that in isolation.

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "snapshot fx rate at buyer checkout for cross-currency orders"
```

---

## Task 9: Refund uses snapshot rate

**Files:**
- Modify: `internal/module/order/biz/refund.go`
- Modify: `internal/module/order/biz/timeout.go` (both `CancelUnpaidCheckout` and `AutoCancelPendingItems`)
- Modify: `internal/module/order/biz/checkout.go` (`CancelBuyerPending`)
- Modify: `internal/module/order/biz/incoming.go` (the refund branch at line ~438 per earlier grep)

- [ ] **Step 1: Extract common refund helper**

Add a helper at the bottom of `refund.go`:

```go
// refundBuyerWallet credits the buyer in buyer-currency units.
// totalBuyerCurrency is already in buyer currency; no conversion needed.
func (b *OrderHandler) refundBuyerWallet(ctx restate.Context, buyerID uuid.UUID, totalBuyerCurrency int64, paymentID int64) error {
	if totalBuyerCurrency <= 0 {
		return nil
	}
	return b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
		AccountID: buyerID,
		Amount:    totalBuyerCurrency,
		Type:      "Refund",
		Reference: fmt.Sprintf("payment-%d", paymentID),
		Note:      "refund from payment",
	})
}
```

This keeps existing refund behaviour but consolidates the pattern.

- [ ] **Step 2: Verify `order.item.paid_amount` semantics**

Grep for where `paid_amount` is written during checkout:

```bash
rg -n 'paid_amount|PaidAmount' internal/module/order/biz/checkout.go
```

If `paid_amount` is written in buyer currency (it should be, since it comes from the user-facing `total`), no conversion is needed at refund time. Document this in a one-line comment above `refundBuyerWallet`.

If `paid_amount` is in seller currency (discovered only at this step), add a conversion using `payment.exchange_rate`:

```go
buyerAmount := int64(math.Round(payment.ExchangeRate.InexactFloat64() * float64(sellerAmount)))
```

- [ ] **Step 3: Replace inline WalletCredit calls with helper**

In `timeout.go::CancelUnpaidCheckout` (around line 105), `timeout.go::AutoCancelPendingItems` (around line 199), `incoming.go` (around line 438), and `checkout.go::CancelBuyerPending` — replace the direct `b.account.WalletCredit(...)` calls with `b.refundBuyerWallet(ctx, buyerID, totalRefund, paymentID)`.

- [ ] **Step 4: Guard against stale rate at refund time**

At each refund site, load the payment row (or pass the already-loaded one) to confirm buyer currency matches the expected wallet currency. Since `Infer(buyer.country)` is stable and payment snapshots it, this is mostly a sanity check:

```go
paymentCurrency := payment.BuyerCurrency
expected, _ := sharedcurrency.Infer(buyerCountry)
if paymentCurrency != expected {
	// Log warning; country must have changed, which should have been blocked by the balance guard.
	slog.Warn("refund currency drift", slog.String("payment", paymentCurrency), slog.String("current", expected))
}
```

- [ ] **Step 5: Build + run existing order tests**

```bash
go build ./...
go test ./internal/module/order/...
```

Expected: green. Fix any newly surfaced failures.

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "centralise refund wallet credit and honour payment currency snapshot"
```

---

## Task 10: Frontend — signup country dropdown

**Files (in sibling repo `../shopnexus-website-v3/`):**
- Modify: the signup form component (search for the signup page route or form name).
- Modify: the API client hook for signup to include `country` in the payload.

- [ ] **Step 1: Locate signup form**

From `../shopnexus-website-v3/`, run:

```bash
rg -n 'signup|register' app/ core/ --type ts --type tsx
```

Open the signup form file.

- [ ] **Step 2: Add country select input**

Add a `<Select>` or shadcn `<Combobox>` with ISO 3166 alpha-2 options. Use `Intl.DisplayNames` for localized names:

```tsx
const countries = React.useMemo(() => {
  const regionNames = new Intl.DisplayNames(['en'], { type: 'region' });
  return ['VN','US','GB','DE','FR','JP','KR','TH','SG','MY','ID','PH','AU','CA','IN']
    .map(code => ({ code, name: regionNames.of(code) }));
}, []);
```

Hook into form state and include `country` in the submitted payload. Mark it required.

- [ ] **Step 3: Update API client types**

In the signup mutation hook file (search `core/account/signup.ts` or similar), add `country: string` to the request type, and pass it to `customFetch` as part of the POST body.

- [ ] **Step 4: Manual browser smoke test**

Run `bun dev`, open `http://localhost:5006/signup`, try to submit without country (expect validation error), then submit with a valid country (expect success + infer_currency set on profile).

- [ ] **Step 5: Commit (from website repo)**

```bash
cd ../shopnexus-website-v3
git add -u
git commit -m "add country select to signup form"
cd -
```

---

## Task 11: Frontend — profile country edit

**Files (in `../shopnexus-website-v3/`):**
- Modify: profile settings page / account settings page.
- Modify: profile mutation hooks.

- [ ] **Step 1: Add country row to settings UI**

Find the existing preferred_currency row in the profile settings page. Below it, add a country row with:
- Current country label.
- Edit button opening a dialog with a Select of supported countries.
- Save button calls the new PATCH `/profile/country` endpoint.

- [ ] **Step 2: Handle 409 wallet_not_empty response**

In the mutation onError, if the API returns `409` with `code: "wallet_not_empty"`, show a toast explaining: "Your wallet has a non-zero balance. Spend or withdraw it before changing country."

- [ ] **Step 3: Add API hook**

In `core/account/profile.ts` (or similar), add:

```ts
export function useUpdateCountry() {
  return useMutation({
    mutationFn: (country: string) =>
      customFetch.PATCH('/profile/country', { body: { country } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['profile'] }),
  });
}
```

- [ ] **Step 4: Browser smoke test**

Confirm edit-with-empty-wallet succeeds, edit-with-non-empty-wallet shows the toast.

- [ ] **Step 5: Commit**

```bash
cd ../shopnexus-website-v3
git add -u
git commit -m "add country edit to profile settings"
cd -
```

---

## Task 12: Frontend — checkout FX preview

**Files (in `../shopnexus-website-v3/`):**
- Modify: checkout page or buy-now dialog.

- [ ] **Step 1: Show FX conversion line when cross-currency**

When the buyer's preferred (wallet) currency differs from the product's currency, render a subtle "You pay: X BUYER_CURRENCY ≈ Y SELLER_CURRENCY at rate 1 SELLER = Z BUYER" row above the Confirm button.

Use the already-present `formatMoney` / `convertMoney` helpers. Pull the current rate from the exchange rate hook.

- [ ] **Step 2: Lock the rate at confirm time**

Ensure the rate shown at confirm click is the one submitted with the checkout request — backend will snapshot whichever rate is cached when it receives the request. The FE rate shown is advisory; backend truth is authoritative. No special locking needed client-side, but add a note that the rate may change by up to 15 minutes (cache TTL).

- [ ] **Step 3: Browser smoke test**

Test checkout flow with a VND buyer buying a USD product (seed data as needed).

- [ ] **Step 4: Commit**

```bash
cd ../shopnexus-website-v3
git add -u
git commit -m "show fx preview at cross-currency checkout"
cd -
```

---

## Final Verification

- [ ] **Run the full backend test suite**

```bash
go test ./...
```

Expected: green.

- [ ] **Run a full E2E smoke scenario manually**

1. Sign up buyer in VN → wallet currency VND.
2. Sign up seller in US → wallet currency USD, products auto USD.
3. Buyer checks out USD product → payment row has `buyer_currency=VND`, `seller_currency=USD`, `exchange_rate` snapshotted.
4. Cancel the order pre-seller-confirm → buyer gets VND credit back equal to the original charge.
5. Try to update buyer country to US with wallet balance > 0 → 409.
6. Drain buyer wallet (spend or refund-then-withdraw), retry country update → 200.

- [ ] **Update CLAUDE.md if new invariants need documenting**

Consider adding a line under "Conventions" noting: "Wallet currency is always `Infer(profile.country)`; to change country, wallet balance must be zero."

- [ ] **Final commit (if any doc updates)**

```bash
git add -u
git commit -m "document multi-currency wallet invariants"
```
