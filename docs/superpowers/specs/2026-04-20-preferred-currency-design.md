# Preferred Currency: Profile Settings + FE Multi-Currency Display

**Date:** 2026-04-20
**Status:** Approved

## Summary

Add a user-level `preferred_currency` preference stored in a new `account.profile.settings` JSONB column. Drop the unused `account.account.settings` column. Introduce a backend exchange-rate pipeline (`common.exchange_rate` table + Frankfurter provider + goroutine cron). Refactor all frontend price displays to show both native and converted currency when they differ, with emphasis rules tuned per surface (browse vs. checkout vs. seller).

## Motivation

- `account.account.settings` JSONB exists but is unused; belongs on profile (user preference), not on account (auth/identity).
- Catalog SPUs already have `currency VARCHAR(3)` (added by `2026-04-18-currency-refactor-design.md`), so the marketplace is data-level multi-currency — but the FE ignores this, defaulting most prices to VND via `formatPrice(x)` with no currency arg. Result: silently incorrect display for non-VND products.
- Users should be able to browse in their preferred currency while still seeing the authoritative native price, especially at checkout where the actual charge currency matters.

## Supersedes

- **`2026-04-18-currency-refactor-design.md`** — specifically the "`account_account.settings JSONB` holds `{"currency": "VND"}`" plan. That column was never populated; this design drops it and introduces `account.profile.settings` with typed `preferred_currency` field instead.

## Scope

**In scope:**
- Schema: drop `account.account.settings`, add `account.profile.settings` JSONB, add `common.exchange_rate` table
- Backend: exchange-rate fetch cron (plain goroutine + `time.Ticker`, not Restate cron) + API endpoint + profile settings patch API
- Frontend: money utilities, `<Price>` component, currency picker, migrate all 71 `formatPrice` call sites

**Out of scope (display-only):**
- Multi-currency checkout (user pays in native, not preferred_currency)
- FX locking on orders (historical rates frozen per order)
- Multi-currency wallet (wallet stays single-currency VND)
- `Money` shared struct (deferred — `2026-04-18-money-struct-design.md` is independent, still pending)

## Design

### 1. Schema changes (edit init migrations in place — dev stage)

#### `internal/module/account/db/migrations/0001_init.up.sql`

Remove `settings` from `account.account`:

```diff
 CREATE TABLE IF NOT EXISTS "account"."account" (
     ...
     "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
-    "settings" JSONB NOT NULL DEFAULT '{}',
     CONSTRAINT "account_pkey" PRIMARY KEY ("id")
 );
```

Add `settings` + CHECK constraint on `account.profile`:

```diff
 CREATE TABLE IF NOT EXISTS "account"."profile" (
     ...
     "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
+    -- User preferences: preferred_currency (ISO 4217) and future prefs
+    -- (theme, notifications, language). App-level validated.
+    "settings" JSONB NOT NULL DEFAULT '{"preferred_currency": "VND"}',
+    CONSTRAINT "profile_settings_preferred_currency_chk" CHECK (
+        NOT (settings ? 'preferred_currency') OR
+        (jsonb_typeof(settings->'preferred_currency') = 'string'
+         AND settings->>'preferred_currency' ~ '^[A-Z]{3}$')
+    ),
     CONSTRAINT "profile_pkey" PRIMARY KEY ("id")
 );
```

Mirror inverses in `0001_init.down.sql`.

#### `internal/module/common/db/migrations/0001_init.up.sql`

Add `common.exchange_rate`:

```sql
CREATE TABLE IF NOT EXISTS "common"."exchange_rate" (
    "base"         VARCHAR(3)      NOT NULL,
    "target"       VARCHAR(3)      NOT NULL,
    "rate"         NUMERIC(20, 10) NOT NULL,  -- amount_in_target = amount_in_base * rate
    "fetched_at"   TIMESTAMPTZ     NOT NULL,  -- upstream fetch time, not DB write
    "date_updated" TIMESTAMPTZ     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "exchange_rate_pkey" PRIMARY KEY ("base", "target"),
    CONSTRAINT "exchange_rate_base_format_chk"   CHECK ("base"   ~ '^[A-Z]{3}$'),
    CONSTRAINT "exchange_rate_target_format_chk" CHECK ("target" ~ '^[A-Z]{3}$'),
    CONSTRAINT "exchange_rate_rate_positive_chk" CHECK ("rate" > 0)
);

CREATE INDEX IF NOT EXISTS "exchange_rate_target_idx"
    ON "common"."exchange_rate" ("target");

-- Seed conservative fallback rates so FE has data before first cron tick.
-- `fetched_at = epoch` triggers "stale" tooltip until refreshed.
INSERT INTO "common"."exchange_rate" (base, target, rate, fetched_at) VALUES
    ('USD', 'VND', 25000,   '1970-01-01'::timestamptz),
    ('USD', 'JPY', 155,     '1970-01-01'::timestamptz),
    ('USD', 'KRW', 1350,    '1970-01-01'::timestamptz),
    ('USD', 'EUR', 0.92,    '1970-01-01'::timestamptz),
    ('USD', 'GBP', 0.78,    '1970-01-01'::timestamptz),
    ('USD', 'CNY', 7.3,     '1970-01-01'::timestamptz),
    ('USD', 'SGD', 1.35,    '1970-01-01'::timestamptz),
    ('USD', 'THB', 35,      '1970-01-01'::timestamptz),
    ('USD', 'AUD', 1.52,    '1970-01-01'::timestamptz)
ON CONFLICT (base, target) DO NOTHING;
```

After edits: `make pgtempl` (with `-skip-schema-prefix -single-file=generated_queries.sql`) + `sqlc generate`.

Dev reset: `docker compose -f deployment/docker-compose.yml down -v && up -d && make migrate && make seed`.

### 2. Backend — exchange rate pipeline

#### 2.1 Provider — `internal/provider/exchange/`

Follows the geocoding provider pattern (interface + impl in separate files).

**`exchange.go`:**
```go
package exchange

type Snapshot struct {
    Base      string
    Rates     map[string]float64  // base * rate = target (base itself NOT included)
    FetchedAt time.Time
}

type Client interface {
    FetchLatest(ctx context.Context, base string, targets []string) (Snapshot, error)
}
```

**`frankfurter.go`:**
- Hits `https://api.frankfurter.dev/v1/latest?base=USD&symbols=VND,JPY,...`
- Free, no API key, ECB-sourced, daily updates
- 5s HTTP timeout, caller handles retry

#### 2.2 Biz — `common/biz/exchange_rate.go`

Add to `CommonBiz` interface:
```go
GetExchangeRates(ctx context.Context) (commonmodel.ExchangeRateSnapshot, error)
ConvertAmount(ctx context.Context, params ConvertAmountParams) (int64, error)
IsSupportedCurrency(ctx context.Context, currency string) bool
```

**Model:**
```go
type ExchangeRateSnapshot struct {
    Base      string             `json:"base"`
    Rates     map[string]float64 `json:"rates"`
    FetchedAt time.Time          `json:"fetched_at"`
    Supported []string           `json:"supported"`  // from config whitelist
}

type ConvertAmountParams struct {
    Amount   int64
    From, To string
}
```

**`ConvertAmount` logic** (BE helper for cross-currency filter/search/analytics):
- Same currency → return as-is
- Load rates from DB (Postgres direct — 10 rows, no Redis needed)
- Compose via USD base: `rate(From→To) = rates[To] / rates[From]`
- Smallest-unit math with decimal scaling (see decimals table below)

**Decimals per currency** (hardcoded in `common/biz/currency_decimals.go`):
```go
var decimals = map[string]int{
    "VND": 0, "USD": 2, "JPY": 0, "KRW": 0, "EUR": 2,
    "GBP": 2, "CNY": 2, "SGD": 2, "THB": 2, "AUD": 2,
}
```

**Cron — `RefreshExchangeRates`:**
- Pattern: mirror `catalog/biz/search_sync.go:SetupCron` — plain goroutine with `time.Ticker`, **not** Restate cron (this codebase uses Restate for request-path sagas, not for periodic jobs)
- Interval: `config.App.Exchange.RefreshInterval` (default 6h)
- Fetch immediately on startup, then tick
- Fetch fail → log warn, keep existing rows
- UPSERT all rates in one batch, ordered by target
- Started from `CommonHandler.SetupCron` called during app lifecycle (same hook as catalog search sync)

#### 2.3 Transport — `common/transport/echo/exchange_rate.go`

```
GET /api/v1/common/currencies/rates
Response: {
  "base": "USD",
  "rates": {"VND": 24500, "JPY": 155, ...},
  "fetched_at": "2026-04-20T06:00:00Z",
  "supported": ["VND","USD","JPY","KRW","EUR","GBP","CNY","SGD","THB","AUD"]
}
```

No auth required (public info). Register in `common.go`:
```go
api.GET("/currencies/rates", h.GetExchangeRates)
```

#### 2.4 SQLC queries — `common/db/queries/exchange_rate.sql`

```sql
-- name: GetExchangeRatesByBase :many
SELECT base, target, rate, fetched_at FROM "common"."exchange_rate"
WHERE base = $1;

-- name: UpsertExchangeRate :exec
INSERT INTO "common"."exchange_rate" (base, target, rate, fetched_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (base, target) DO UPDATE
SET rate = EXCLUDED.rate,
    fetched_at = EXCLUDED.fetched_at,
    date_updated = CURRENT_TIMESTAMP;
```

#### 2.5 Config additions

`config.App.Exchange`:
```yaml
exchange:
  refresh_interval: 6h
  supported: ["VND","USD","JPY","KRW","EUR","GBP","CNY","SGD","THB","AUD"]
  base: "USD"
  http_timeout: 5s
  default_user_currency: "VND"
```

#### 2.6 Wiring

- `fx.go` in common module: `fx.Provide(exchange.NewFrankfurter)` → `exchange.Client`
- `CommonHandler.NewcommonBiz` accepts `exchange.Client`
- Lifecycle: `handler.SetupCron()` called from app startup (follow catalog pattern)

### 3. Backend — account profile settings

#### 3.1 Model — `account/model/account.go`

```go
type Profile struct {
    ...existing fields...
    Settings ProfileSettings `json:"settings"`
}

// ProfileSettings is a typed view of account.profile.settings JSONB.
// Unknown fields in DB are ignored on read; fields not set here are not writable.
type ProfileSettings struct {
    PreferredCurrency string `json:"preferred_currency,omitempty"`
}
```

**Rationale:** typed view gives API contract, TS types on FE, and prevents schema drift. Unknown JSONB fields are preserved on `UpdateProfileSettings` via "load, merge, write" pattern.

#### 3.2 Biz — `account/biz/profile.go`

New method:
```go
type UpdateProfileSettingsParams struct {
    Issuer            accountmodel.AuthenticatedAccount
    AccountID         uuid.UUID   `validate:"required"`
    PreferredCurrency null.String `validate:"omitempty,iso4217"`
}

func (b *AccountHandler) UpdateProfileSettings(
    ctx restate.Context,
    params UpdateProfileSettingsParams,
) (accountmodel.ProfileSettings, error) {
    // 1. Authorization: only self
    // 2. Load current settings, preserve unknown keys
    // 3. Apply patch (null.String semantics: !Valid = don't change)
    // 4. Validate against common.IsSupportedCurrency whitelist
    // 5. Persist via UpdateProfileSettings SQLC query
}
```

**Patch semantics:** `null.String` with `Valid=false` = unchanged; `Valid=true` = set.

#### 3.3 SQLC query

```sql
-- name: UpdateProfileSettings :one
UPDATE "account"."profile"
SET "settings" = $2,
    "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = $1
RETURNING *;
```

#### 3.4 Transport — `account/transport/echo/me.go`

```
PATCH /api/v1/account/me/settings
Body:     { "preferred_currency": "USD" }
Response: { "preferred_currency": "USD" }
```

AccountID from JWT claims. Separate from `PATCH /me/profile` — different cache invalidation scope, different mutation hook on FE.

#### 3.5 Remove `account.account.settings` references

After `sqlc generate` drops the `Settings` field from `AccountAccount`, compiler errors will surface call sites:
- `accountdb/sqlc/models.go` — auto-removed
- `accountdb/sqlc/copyfrom.go:42,51` — auto-removed
- `accountdb/sqlc/batch.go` — auto-removed
- `account/biz/auth.go:Register` — remove if there's any explicit settings set
- Any other biz code — fix per compiler

#### 3.6 Validator — custom `iso4217` tag

Add to `internal/shared/validator/`:
- Regex `^[A-Z]{3}$` + whitelist check via `common.IsSupportedCurrency`
- Register with `go-playground/validator` v10 (already in use)

### 4. Frontend — architecture

#### 4.1 `lib/money.ts` (new — zero-dep, Intl-based)

Replaces `formatPrice` in `lib/utils.ts`:

```ts
const DECIMALS: Record<string, number> = {
  VND: 0, USD: 2, JPY: 0, KRW: 0, EUR: 2,
  GBP: 2, CNY: 2, SGD: 2, THB: 2, AUD: 2,
}
const getDecimals = (c: string) => DECIMALS[c] ?? 2

export function formatMoney(amount: number, currency: string, locale?: string): string
export function convertMoney(amount: number, from: string, to: string,
                             ratesFromUSD: Record<string, number>): number
export function getCurrencyName(currency: string, locale?: string): string
export function formatPriceInline(amount: number, currency: string,
                                  preferred: string,
                                  rates: Record<string, number> | undefined): string
```

`convertMoney` logic:
- Same currency → identity
- Missing rate → return original (don't lie, degrade silently)
- Convert through USD: `major_from → USD major → target major → smallest unit`
- Round only at final step

#### 4.2 `core/common/currency.ts` (new — hooks)

```ts
export type ExchangeRateSnapshot = {
  base: string
  rates: Record<string, number>
  fetched_at: string | null
  supported: string[]
}

export const useExchangeRates = () =>
  useQuery({
    queryKey: ['common', 'exchange-rates'],
    queryFn: () => customFetchStandard<ExchangeRateSnapshot>('common/currencies/rates'),
    staleTime: 60 * 60 * 1000,   // 1h
    gcTime:    24 * 60 * 60 * 1000,
    refetchOnWindowFocus: false,
  })

export function usePreferredCurrency(): string {
  // Fallback chain: profile.settings.preferred_currency > localStorage > "VND"
}

export const useUpdatePreferredCurrency = () =>
  useMutation({
    mutationFn: (currency: string) =>
      customFetchStandard('account/me/settings', {
        method: 'PATCH',
        body: JSON.stringify({ preferred_currency: currency }),
      }),
    onSuccess: (_, currency) => {
      localStorage.setItem('preferred_currency', currency)
      queryClient.invalidateQueries({ queryKey: ['account', 'me'] })
    },
  })
```

#### 4.3 `components/ui/price.tsx` (new — the core display component)

```tsx
type Emphasis = 'preferred' | 'native' | 'native-only'

type PriceProps = {
  amount: number                // smallest-unit integer
  currency: string              // ISO 4217
  emphasis?: Emphasis           // default 'preferred'
  hideConverted?: boolean
  showRateHint?: boolean        // append "at current rate" under converted
  className?: string
}

export function Price(props: PriceProps)
```

Behavior:
- `currency === preferred_currency` → 1 line (native)
- rates not loaded → 1 line (native), no flash; converted fades in once data arrives
- `native-only` → always 1 line (for seller dashboards)
- `preferred` emphasis → converted primary, native muted below
- `native` emphasis → native primary, converted muted below
- `showRateHint` → appends "at current rate" to converted

#### 4.4 `components/ui/currency-picker.tsx` (new)

shadcn `<Command>` + `<Popover>` + flag icons via `country-flag-icons`.

- "Popular" group: VND, USD, JPY, KRW, EUR
- Rest alphabetical
- Currency→country mapping hardcoded for 10 supported currencies
- Localized names via `Intl.DisplayNames`

#### 4.5 Remove `formatPrice`

Delete from `lib/utils.ts`:
- `formatPrice` function
- `currencyLocaleMap` constant

TS compiler surfaces every call site → replace with `<Price>` (or `formatPriceInline` for string contexts).

### 5. Frontend — display migration map

**71 occurrences across 16 files.** Emphasis per surface:

| Surface | Files | Emphasis |
|---|---|---|
| Product card | `components/product/product-card.tsx` | `preferred` |
| Product detail | `app/(marketplace)/product/[slug]/page.tsx` | `preferred` |
| Cart sheet | `components/cart/cart-sheet.tsx` | `preferred` |
| Cart page | `app/(marketplace)/cart/page.tsx` | `preferred` |
| Checkout | `app/(marketplace)/checkout/page.tsx` | **`native`** |
| Buyer order history | `app/(marketplace)/account/orders/page.tsx` + `_components/*` | **`native`** + `showRateHint` |
| Seller dashboard | `app/(marketplace)/seller/page.tsx` | `native-only` |
| Seller products | `app/(marketplace)/seller/products/page.tsx` + `[id]/edit/page.tsx` | `native-only` |
| Seller orders | `app/(marketplace)/seller/orders/page.tsx` + `[id]/page.tsx` | `native-only` |

**Non-JSX contexts** (toasts, button labels, template strings):
- Use `formatPriceInline(amount, currency, preferred, rates)` — returns plain string
- Sites: `product/[slug]/page.tsx:310` (toast), `checkout/page.tsx:456,544` (Pay button)

**Wallet** (`checkout/page.tsx:431,436`):
- `currency="VND"` fixed (wallet is single-currency)
- `emphasis="native"` (VND is the truth)

**Settings page** (`account/settings/_components/preferences-card.tsx:49-52`):
- Replace hardcoded "USD ($)" with `<CurrencyPicker>` component

### 6. Mixed-currency cart (option A — group by currency)

**Problem:** cart items from multiple sellers with different currencies cannot be summed directly (₫ + ¥ = nonsense).

**Solution:** group items by `sku.spu.currency`, show per-group subtotals, optional grand total in preferred currency:

```
Subtotal:
  ₫150,000  (2 items from Shop A)
  ¥5,000    (1 item from Shop B)

Grand total (approximate):
  ≈ $40.20
```

Implementation:
```tsx
const groups = _.groupBy(items, i => i.sku.spu.currency)
// Render one subtotal block per group, all using <Price emphasis="preferred">
// Grand total: sum of converted amounts, using <Price emphasis="preferred" showRateHint>
```

**Aligns with backend:** orders are already split per-seller → each order has one currency. Grouping in cart is UI-only, no backend change.

**Same-currency cart (normal case):** single subtotal block — backward compatible.

### 7. Error handling

| Scenario | Behavior |
|---|---|
| Frankfurter 5xx / timeout | Log warn, keep existing rows, retry next tick |
| Partial currency response | Upsert present rates, log missing once per cycle |
| `fetched_at > 48h` | Log warn hourly; endpoint still serves |
| FE `/rates` fails | TanStack retry 3x; degrade to `native-only` silently |
| `rates[preferred]` missing | `convertMoney` returns original → effective `native-only` |
| `fetched_at > 24h` | Tooltip on converted line: "Rate last updated X ago" |
| Invalid `preferred_currency` on profile | BE fallback on load + log warn; unsupported → treat as no-conversion for that user |
| LocalStorage preferred not in whitelist | Clear + fallback VND |
| User changes currency mid-checkout | Invalidate `['account','me']`; Prices re-render; cart data untouched |
| Rate cron race (2 instances) | UPSERT is idempotent; no coordination needed |

**Never block checkout on rate fetch failure** — checkout only needs native currency.

### 8. Config defaults

- `exchange.refresh_interval`: 6h
- `exchange.http_timeout`: 5s
- `exchange.default_user_currency`: VND (used on Register to seed profile settings)
- `exchange.supported`: 10 currencies (VND, USD, JPY, KRW, EUR, GBP, CNY, SGD, THB, AUD)

### 9. Testing strategy

**BE unit tests:**
- `provider/exchange/frankfurter_test.go`: happy/5xx/timeout/malformed-json
- `common/biz/exchange_rate_test.go`: `ConvertAmount` same/cross/missing, decimal scaling, cron upsert, cron-failure preservation
- `account/biz/profile_test.go`: `UpdateProfileSettings` valid/invalid/unauthorized/patch-semantics/unknown-keys-preserved

**FE unit tests:**
- `lib/money.test.ts`: `formatMoney`/`convertMoney`/`getCurrencyName` across currencies
- `components/ui/price.test.tsx`: same-currency, both-shown, rates-loading, emphasis variants, `hideConverted`, `showRateHint`

**Manual QA (mandatory before merge — per CLAUDE.md "start dev server, verify in browser"):**
- [ ] Browse as VND-preferred user → VND products single line, non-VND products show both
- [ ] Change to USD in settings → reflected across all open tabs after refetch
- [ ] Product detail: price range, strikethrough original, SKU selection — all correct
- [ ] Mixed-currency cart → grouped subtotals + grand total approximation
- [ ] Checkout: native dominant, converted muted, "You will be charged in X" hint
- [ ] Buyer order history: native dominant, `at current rate` hint present
- [ ] Seller dashboard: native only, no converted line anywhere
- [ ] Wallet (preferred ≠ VND): show both
- [ ] Wallet (preferred = VND): single line
- [ ] Rate fetch fail (throttle network) → Prices gracefully show native only, no JS errors
- [ ] Invalid `preferred_currency` via direct API → BE rejects with `ErrUnsupportedCurrency`

## Files affected

**Backend (new):**
- `internal/provider/exchange/exchange.go`
- `internal/provider/exchange/frankfurter.go`
- `internal/module/common/biz/exchange_rate.go`
- `internal/module/common/biz/currency_decimals.go`
- `internal/module/common/transport/echo/exchange_rate.go`
- `internal/module/common/db/queries/exchange_rate.sql`
- `internal/module/common/model/exchange_rate.go`

**Backend (modified):**
- `internal/module/account/db/migrations/0001_init.up.sql` + `.down.sql`
- `internal/module/common/db/migrations/0001_init.up.sql` + `.down.sql`
- `internal/module/account/model/account.go`
- `internal/module/account/biz/profile.go`
- `internal/module/account/biz/interface.go`
- `internal/module/account/biz/auth.go` (Register flow)
- `internal/module/account/db/queries/*.sql` (regen)
- `internal/module/account/transport/echo/me.go`
- `internal/module/common/biz/interface.go`
- `internal/module/common/biz/fx.go` (add exchange.Client provider)
- `internal/module/common/transport/echo/common.go` (route registration)
- `internal/shared/validator/*.go` (add iso4217 tag)
- `config/*.yaml` (add `exchange` section)

**Frontend (new):**
- `lib/money.ts`
- `core/common/currency.ts`
- `components/ui/price.tsx`
- `components/ui/currency-picker.tsx`

**Frontend (modified — 16 display files):**
- `components/product/product-card.tsx`
- `components/cart/cart-sheet.tsx`
- `app/(marketplace)/product/[slug]/page.tsx`
- `app/(marketplace)/cart/page.tsx`
- `app/(marketplace)/checkout/page.tsx`
- `app/(marketplace)/account/orders/page.tsx`
- `app/(marketplace)/account/orders/_components/order-list.tsx`
- `app/(marketplace)/account/orders/[id]/_components/order-items-card.tsx`
- `app/(marketplace)/account/orders/[id]/_components/order-summary-card.tsx`
- `app/(marketplace)/seller/page.tsx`
- `app/(marketplace)/seller/products/page.tsx`
- `app/(marketplace)/seller/products/[id]/edit/page.tsx`
- `app/(marketplace)/seller/orders/page.tsx`
- `app/(marketplace)/seller/orders/[id]/page.tsx`
- `app/(marketplace)/account/settings/_components/preferences-card.tsx`
- `core/account/account.ts` (add `settings` to `AccountProfile` type + hook)

**Frontend (removed):**
- `formatPrice` and `currencyLocaleMap` from `lib/utils.ts`

**Docs:**
- `shopnexus-website-v3/CLAUDE.md` — update price utility references
- `internal/module/common/README.md` — add exchange rate section
- `internal/module/account/README.md` — add profile settings section

## Rollout

**Big-bang** (dev stage, no prod users):
1. Edit init migrations
2. `docker compose down -v && up -d && make migrate && make seed`
3. Regenerate SQLC bindings
4. Implement backend changes
5. Implement frontend changes
6. Manual QA checklist
7. Single PR merge

**Rollback** (if needed): git revert + dev DB reset.

## Open questions resolved

- **Region vs country vs preferred_currency?** → Only `preferred_currency`. Country exists on `account.contact` already; no new field needed.
- **Where to store?** → `account.profile.settings` JSONB (not `account.account.settings`, which is being dropped).
- **Conversion scope?** → Display-only. Native currency remains authoritative for transactions.
- **Conversion architecture?** → FE-computed from rate table exposed by BE `/common/currencies/rates`.
- **Rate source?** → Frankfurter (free, no key, ECB daily) + self-hosted cache refreshed by a goroutine + `time.Ticker` cron (the catalog search-sync pattern).
- **Library on FE?** → Zero-dep (Intl APIs) + `country-flag-icons` for picker visuals only.
- **Emphasis rules?** → Browse/cart: preferred primary; checkout/order/wallet: native primary; seller dashboard: native only.
- **Mixed-currency cart?** → Group by currency (option A) with per-group subtotals + approximate grand total.
- **Seller dashboard convert?** → No. Native only.
- **Currency module path?** → `common` module (not a new top-level module).
