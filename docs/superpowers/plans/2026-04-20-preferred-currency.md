# Preferred Currency + Multi-Currency Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move user-level `preferred_currency` into `account.profile.settings` JSONB, introduce a backend exchange-rate pipeline (Frankfurter + goroutine cron + DB cache), and migrate every frontend price display to show both native and converted amounts per the emphasis rules defined in the spec.

**Architecture:** Big-bang refactor on dev stage — edit `0001_init.up.sql` in place, reset dev DB, regenerate SQLC. Backend exposes one rate endpoint; frontend converts client-side using zero-dep Intl APIs. Emphasis rules differ per surface (browse vs checkout vs seller). Cart handles mixed-currency items via per-currency grouping.

**Tech Stack:** Go 1.23+, sqlc, Restate SDK, Echo v4, TanStack Query, Next.js 16, Intl APIs, `country-flag-icons`, Frankfurter API.

**Spec:** `docs/superpowers/specs/2026-04-20-preferred-currency-design.md`

---

## File Structure

### Backend (new)
- `internal/provider/exchange/exchange.go` — Client interface + Snapshot type
- `internal/provider/exchange/frankfurter.go` — Frankfurter impl
- `internal/provider/exchange/frankfurter_test.go` — provider tests
- `internal/module/common/biz/exchange_rate.go` — RefreshRates, GetRates, Convert, IsSupportedCurrency
- `internal/module/common/biz/exchange_rate_test.go` — biz tests
- `internal/module/common/biz/currency_decimals.go` — decimals map + helpers
- `internal/module/common/model/exchange_rate.go` — ExchangeRateSnapshot
- `internal/module/common/transport/echo/exchange_rate.go` — GET /currencies/rates
- `internal/module/common/db/queries/exchange_rate.sql` — SQLC queries
- `internal/shared/validator/iso4217.go` — custom iso4217 validator tag
- `config/default.yaml` exchange section

### Backend (modified)
- `internal/module/account/db/migrations/0001_init.{up,down}.sql` — drop account.settings, add profile.settings + CHECK
- `internal/module/common/db/migrations/0001_init.{up,down}.sql` — add exchange_rate table + seed
- `internal/module/account/model/account.go` — add `Settings ProfileSettings`
- `internal/module/account/biz/interface.go` — add UpdateProfileSettings
- `internal/module/account/biz/profile.go` — add UpdateProfileSettings method + dbToProfile settings parse
- `internal/module/account/biz/auth.go` — Register flow default settings
- `internal/module/account/transport/echo/me.go` — PATCH /me/settings
- `internal/module/common/biz/interface.go` — add exchange methods
- `internal/module/common/biz/interface.go` (CommonHandler struct) — add exchange.Client field + SetupCron
- `internal/module/common/fx.go` — wire exchange.Client provider + lifecycle
- `internal/module/common/transport/echo/common.go` — register /currencies/rates route
- `internal/app/app.go` — call common.SetupCron() on startup (follow catalog pattern)

### Frontend (new)
- `lib/money.ts` — formatMoney, convertMoney, getCurrencyName, formatPriceInline
- `lib/money.test.ts` — money utility tests
- `core/common/currency.ts` — useExchangeRates, usePreferredCurrency, useUpdatePreferredCurrency
- `components/ui/price.tsx` — <Price> component
- `components/ui/price.test.tsx` — Price component tests
- `components/ui/currency-picker.tsx` — CurrencyPicker (Command + Popover + flags)

### Frontend (modified)
- `lib/utils.ts` — remove formatPrice + currencyLocaleMap
- `core/account/account.ts` — add `settings: ProfileSettings` to AccountProfile type + useUpdatePreferredCurrency hook
- 16 display files (see Tasks 20–26)

---

## Phase 1 — Schema + Backend Foundation (sequential)

### Task 1: Schema migrations + dev DB reset

**Files:**
- Modify: `internal/module/account/db/migrations/0001_init.up.sql`
- Modify: `internal/module/account/db/migrations/0001_init.down.sql`
- Modify: `internal/module/common/db/migrations/0001_init.up.sql`
- Modify: `internal/module/common/db/migrations/0001_init.down.sql`

- [ ] **Step 1: Remove `settings` column from `account.account`**

In `internal/module/account/db/migrations/0001_init.up.sql`, locate the `CREATE TABLE IF NOT EXISTS "account"."account"` block (around line 26) and delete line 36:

```diff
     "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
-    "settings" JSONB NOT NULL DEFAULT '{}',
     CONSTRAINT "account_pkey" PRIMARY KEY ("id")
 );
```

- [ ] **Step 2: Add `settings` column + CHECK constraint to `account.profile`**

In the same file, add inside `CREATE TABLE IF NOT EXISTS "account"."profile"` block:

```diff
     "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
     "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
+    -- User preferences: preferred_currency (ISO 4217) and future prefs.
+    "settings" JSONB NOT NULL DEFAULT '{"preferred_currency": "VND"}',
+    CONSTRAINT "profile_settings_preferred_currency_chk" CHECK (
+        NOT (settings ? 'preferred_currency') OR
+        (jsonb_typeof(settings->'preferred_currency') = 'string'
+         AND settings->>'preferred_currency' ~ '^[A-Z]{3}$')
+    ),
     CONSTRAINT "profile_pkey" PRIMARY KEY ("id")
 );
```

- [ ] **Step 3: Mirror inverses in account `0001_init.down.sql`**

If the down migration references `account.account.settings`, remove the column drop for it. If it references profile.settings, it should not (profile didn't have it before). Ensure symmetry with new up migration.

- [ ] **Step 4: Add `common.exchange_rate` table + seed in common up migration**

In `internal/module/common/db/migrations/0001_init.up.sql`, append at the end (before any final statements):

```sql
-- =============================================
-- EXCHANGE RATES
-- =============================================
-- Exchange rates fetched from Frankfurter by common.SetupCron.
-- base is always USD in current deployment; (base, target) PK keeps
-- schema flexible for future multi-base storage.
CREATE TABLE IF NOT EXISTS "common"."exchange_rate" (
    "base"         VARCHAR(3)      NOT NULL,
    "target"       VARCHAR(3)      NOT NULL,
    "rate"         NUMERIC(20, 10) NOT NULL,
    "fetched_at"   TIMESTAMPTZ     NOT NULL,
    "date_updated" TIMESTAMPTZ     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "exchange_rate_pkey" PRIMARY KEY ("base", "target"),
    CONSTRAINT "exchange_rate_base_format_chk"   CHECK ("base"   ~ '^[A-Z]{3}$'),
    CONSTRAINT "exchange_rate_target_format_chk" CHECK ("target" ~ '^[A-Z]{3}$'),
    CONSTRAINT "exchange_rate_rate_positive_chk" CHECK ("rate" > 0)
);

CREATE INDEX IF NOT EXISTS "exchange_rate_target_idx"
    ON "common"."exchange_rate" ("target");

-- Seed conservative fallback rates so FE has data before first cron tick.
-- fetched_at = epoch → "stale" tooltip shows until cron refresh.
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

- [ ] **Step 5: Mirror inverse in common `0001_init.down.sql`**

Append to the down migration:

```sql
DROP INDEX IF EXISTS "common"."exchange_rate_target_idx";
DROP TABLE IF EXISTS "common"."exchange_rate";
```

- [ ] **Step 6: Reset dev DB and re-run migrations**

Run: `docker compose -f deployment/docker-compose.yml down -v && docker compose -f deployment/docker-compose.yml up -d`

Wait ~10s for Postgres healthy, then: `make migrate && make seed`

Expected: no migration errors; verify seeded rates:
```bash
docker exec -i shopnexus-postgres psql -U postgres -d shopnexus -c "SELECT base, target, rate FROM common.exchange_rate ORDER BY target;"
```
Expected: 9 rows (USD→VND, JPY, KRW, EUR, GBP, CNY, SGD, THB, AUD).

- [ ] **Step 7: Commit**

```bash
git add internal/module/account/db/migrations/0001_init.up.sql \
        internal/module/account/db/migrations/0001_init.down.sql \
        internal/module/common/db/migrations/0001_init.up.sql \
        internal/module/common/db/migrations/0001_init.down.sql
git commit -m "add profile.settings and common.exchange_rate, drop account.settings"
```

---

### Task 2: Regenerate SQLC + add exchange rate queries

**Files:**
- Create: `internal/module/common/db/queries/exchange_rate.sql`
- Modify: auto-regen `internal/module/account/db/queries/generated_queries.sql`
- Modify: auto-regen `internal/module/account/db/sqlc/*.go`
- Modify: auto-regen `internal/module/common/db/sqlc/*.go`

- [ ] **Step 1: Create exchange rate query file**

Create `internal/module/common/db/queries/exchange_rate.sql`:

```sql
-- name: GetExchangeRatesByBase :many
SELECT "base", "target", "rate", "fetched_at"
FROM "common"."exchange_rate"
WHERE "base" = $1
ORDER BY "target";

-- name: UpsertExchangeRate :exec
INSERT INTO "common"."exchange_rate" ("base", "target", "rate", "fetched_at")
VALUES ($1, $2, $3, $4)
ON CONFLICT ("base", "target") DO UPDATE
SET "rate" = EXCLUDED."rate",
    "fetched_at" = EXCLUDED."fetched_at",
    "date_updated" = CURRENT_TIMESTAMP;

-- name: UpdateProfileSettings :one
UPDATE "account"."profile"
SET "settings" = $2,
    "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = $1
RETURNING *;
```

Note: `UpdateProfileSettings` lives in the **account** queries. Move it to the account module query file instead. Edit: remove the UpdateProfileSettings block from the file above — it stays only in account module.

- [ ] **Step 2: Add UpdateProfileSettings to account queries**

The pgtempl tool auto-generates from migrations, so this query will appear automatically. But if a custom query is needed, create `internal/module/account/db/queries/profile_custom.sql`:

```sql
-- name: UpdateProfileSettings :one
UPDATE "account"."profile"
SET "settings" = $2,
    "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = $1
RETURNING *;
```

Check existing `account/db/queries/` directory for `*_custom.sql` pattern. If none, place in `profile_custom.sql`.

- [ ] **Step 3: Regenerate pgtempl + sqlc**

Run:
```bash
make pgtempl
# Runs: go run ./cmd/pgtempl/ -module account -skip-schema-prefix -single-file=generated_queries.sql
# and same for common module
sqlc generate
```

Expected: no errors. Verify:
- `internal/module/account/db/sqlc/models.go` — `AccountAccount` struct has NO `Settings` field
- `internal/module/account/db/sqlc/models.go` — `AccountProfile` struct HAS `Settings json.RawMessage` field
- `internal/module/common/db/sqlc/models.go` — new `CommonExchangeRate` struct present
- Generated `GetExchangeRatesByBase`, `UpsertExchangeRate`, `UpdateProfileSettings` query functions

- [ ] **Step 4: Commit**

```bash
git add internal/module/account/db/queries \
        internal/module/account/db/sqlc \
        internal/module/common/db/queries \
        internal/module/common/db/sqlc
git commit -m "regenerate sqlc for profile.settings and exchange_rate"
```

---

### Task 3: Exchange provider — Frankfurter client

**Files:**
- Create: `internal/provider/exchange/exchange.go`
- Create: `internal/provider/exchange/frankfurter.go`
- Create: `internal/provider/exchange/frankfurter_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/provider/exchange/frankfurter_test.go`:

```go
package exchange_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shopnexus-server/internal/provider/exchange"
)

func TestFrankfurter_FetchLatest_Success(t *testing.T) {
	body := `{"amount":1.0,"base":"USD","date":"2026-04-20","rates":{"VND":25000.5,"JPY":155.2}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("base") != "USD" {
			t.Fatalf("expected base=USD, got %q", r.URL.Query().Get("base"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := exchange.NewFrankfurter(srv.URL, &http.Client{Timeout: 2 * time.Second})
	snap, err := client.FetchLatest(context.Background(), "USD", []string{"VND", "JPY"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Base != "USD" {
		t.Errorf("Base = %q, want USD", snap.Base)
	}
	if snap.Rates["VND"] != 25000.5 || snap.Rates["JPY"] != 155.2 {
		t.Errorf("rates = %+v", snap.Rates)
	}
}

func TestFrankfurter_FetchLatest_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := exchange.NewFrankfurter(srv.URL, &http.Client{Timeout: 1 * time.Second})
	_, err := client.FetchLatest(context.Background(), "USD", []string{"VND"})
	if err == nil {
		t.Fatal("expected error on 5xx, got nil")
	}
}

func TestFrankfurter_FetchLatest_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	client := exchange.NewFrankfurter(srv.URL, &http.Client{Timeout: 1 * time.Second})
	_, err := client.FetchLatest(context.Background(), "USD", []string{"VND"})
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}
```

- [ ] **Step 2: Run test — should fail to compile**

Run: `go test ./internal/provider/exchange/...`
Expected: build error "undefined: exchange.NewFrankfurter" (package not yet created)

- [ ] **Step 3: Create interface**

Create `internal/provider/exchange/exchange.go`:

```go
// Package exchange provides currency exchange rate lookup against an
// external provider. Implementations must be safe for concurrent use.
package exchange

import (
	"context"
	"time"
)

// Snapshot is one immutable rate lookup result against a base currency.
// Rates map target ISO 4217 codes to multipliers: amount_in_target =
// amount_in_base * Rates[target]. The base currency itself is NOT
// included in Rates (caller handles identity as 1.0).
type Snapshot struct {
	Base      string
	Rates     map[string]float64
	FetchedAt time.Time
}

// Client fetches latest exchange rates from an upstream provider.
type Client interface {
	FetchLatest(ctx context.Context, base string, targets []string) (Snapshot, error)
}
```

- [ ] **Step 4: Create Frankfurter implementation**

Create `internal/provider/exchange/frankfurter.go`:

```go
package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type frankfurterResponse struct {
	Base  string             `json:"base"`
	Date  string             `json:"date"`
	Rates map[string]float64 `json:"rates"`
}

type frankfurterClient struct {
	baseURL string
	http    *http.Client
}

// NewFrankfurter returns a Client backed by the Frankfurter API.
// baseURL is typically "https://api.frankfurter.dev" (no trailing slash).
// httpClient controls timeout and transport; callers should pass one
// with an explicit timeout (e.g. 5s).
func NewFrankfurter(baseURL string, httpClient *http.Client) Client {
	return &frankfurterClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    httpClient,
	}
}

func (c *frankfurterClient) FetchLatest(
	ctx context.Context, base string, targets []string,
) (Snapshot, error) {
	q := url.Values{}
	q.Set("base", base)
	if len(targets) > 0 {
		q.Set("symbols", strings.Join(targets, ","))
	}
	u := fmt.Sprintf("%s/v1/latest?%s", c.baseURL, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("exchange: build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("exchange: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Snapshot{}, fmt.Errorf("exchange: upstream status %d", resp.StatusCode)
	}

	var body frankfurterResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Snapshot{}, fmt.Errorf("exchange: decode: %w", err)
	}

	return Snapshot{
		Base:      body.Base,
		Rates:     body.Rates,
		FetchedAt: time.Now().UTC(),
	}, nil
}
```

- [ ] **Step 5: Run tests — should pass**

Run: `go test ./internal/provider/exchange/... -v`
Expected: all 3 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/exchange/
git commit -m "add frankfurter exchange rate provider"
```

---

### Task 4: Common biz — currency decimals + convert helper

**Files:**
- Create: `internal/module/common/biz/currency_decimals.go`
- Create: `internal/module/common/model/exchange_rate.go`

- [ ] **Step 1: Create decimals helper**

Create `internal/module/common/biz/currency_decimals.go`:

```go
package commonbiz

// currencyDecimals is the ISO 4217 minor-unit exponent for supported
// currencies. Used for smallest-unit <-> major-unit conversion during
// cross-currency math. Callers unaware of a currency MUST default to 2.
var currencyDecimals = map[string]int{
	"VND": 0, "USD": 2, "JPY": 0, "KRW": 0, "EUR": 2,
	"GBP": 2, "CNY": 2, "SGD": 2, "THB": 2, "AUD": 2,
}

// decimalsFor returns the ISO 4217 minor-unit exponent, defaulting to 2.
func decimalsFor(currency string) int {
	if d, ok := currencyDecimals[currency]; ok {
		return d
	}
	return 2
}
```

- [ ] **Step 2: Create model**

Create `internal/module/common/model/exchange_rate.go`:

```go
package commonmodel

import "time"

// ExchangeRateSnapshot is the FE-facing shape of the exchange rate table.
// Base is always USD in current deployment. Rates map ISO 4217 targets
// to multipliers: amount_in_target = amount_in_base * Rates[target].
// Rates does NOT include Base itself (identity handled client-side).
type ExchangeRateSnapshot struct {
	Base      string             `json:"base"`
	Rates     map[string]float64 `json:"rates"`
	FetchedAt *time.Time         `json:"fetched_at"`
	Supported []string           `json:"supported"`
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/module/common/biz/currency_decimals.go \
        internal/module/common/model/exchange_rate.go
git commit -m "add currency decimals and exchange rate snapshot model"
```

---

### Task 5: Common biz — exchange rate business logic

**Files:**
- Create: `internal/module/common/biz/exchange_rate.go`
- Create: `internal/module/common/biz/exchange_rate_test.go`
- Modify: `internal/module/common/biz/interface.go`

- [ ] **Step 1: Write failing tests**

Create `internal/module/common/biz/exchange_rate_test.go`:

```go
package commonbiz_test

import (
	"testing"

	commonbiz "shopnexus-server/internal/module/common/biz"
)

func TestConvertAmount_SameCurrency(t *testing.T) {
	got := commonbiz.ConvertAmountPure(100_000, "VND", "VND", map[string]float64{"VND": 25000})
	if got != 100_000 {
		t.Errorf("same currency: got %d, want 100000", got)
	}
}

func TestConvertAmount_USDtoVND(t *testing.T) {
	// $10.00 = 1000 cents → 10 * 25000 = 250,000 VND (VND has 0 decimals)
	got := commonbiz.ConvertAmountPure(1000, "USD", "VND", map[string]float64{"VND": 25000})
	if got != 250_000 {
		t.Errorf("USD→VND: got %d, want 250000", got)
	}
}

func TestConvertAmount_VNDtoUSD(t *testing.T) {
	// 250,000 VND → 250000/25000 = 10 USD → 1000 cents
	got := commonbiz.ConvertAmountPure(250_000, "VND", "USD", map[string]float64{"VND": 25000})
	if got != 1000 {
		t.Errorf("VND→USD: got %d, want 1000", got)
	}
}

func TestConvertAmount_JPYtoVND(t *testing.T) {
	// 10,000 JPY (0 decimals) → USD: 10000/155 = 64.516... → VND: *25000 = 1,612,903
	got := commonbiz.ConvertAmountPure(10_000, "JPY", "VND",
		map[string]float64{"JPY": 155, "VND": 25000})
	want := int64(1_612_903)
	if got < want-1 || got > want+1 {
		t.Errorf("JPY→VND: got %d, want %d (±1)", got, want)
	}
}

func TestConvertAmount_MissingRate(t *testing.T) {
	// Unknown rate → return amount unchanged (fail-open display)
	got := commonbiz.ConvertAmountPure(100_000, "VND", "XYZ", map[string]float64{"VND": 25000})
	if got != 100_000 {
		t.Errorf("missing rate: got %d, want 100000 (passthrough)", got)
	}
}
```

Note: the biz method will call `ConvertAmountPure` which is the exported pure function (no DB) — testable without mocks.

- [ ] **Step 2: Run tests — should fail to compile**

Run: `go test ./internal/module/common/biz/... -run TestConvertAmount -v`
Expected: "undefined: commonbiz.ConvertAmountPure"

- [ ] **Step 3: Implement exchange_rate.go**

Create `internal/module/common/biz/exchange_rate.go`:

```go
package commonbiz

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"time"

	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonmodel "shopnexus-server/internal/module/common/model"
	"shopnexus-server/internal/provider/exchange"
)

// ConvertAmountParams: amount in smallest unit of From, converted to
// smallest unit of To.
type ConvertAmountParams struct {
	Amount   int64
	From, To string
}

// ConvertAmountPure converts amount through the USD base. ratesFromUSD
// maps target currency to "1 USD = rate target". Returns the original
// amount unchanged when a rate is missing (fail-open; callers display
// original currency). Exported for testability without DB setup.
func ConvertAmountPure(amount int64, from, to string, ratesFromUSD map[string]float64) int64 {
	if from == to {
		return amount
	}
	rateFrom := 1.0
	if from != "USD" {
		r, ok := ratesFromUSD[from]
		if !ok || r <= 0 {
			return amount
		}
		rateFrom = r
	}
	rateTo := 1.0
	if to != "USD" {
		r, ok := ratesFromUSD[to]
		if !ok || r <= 0 {
			return amount
		}
		rateTo = r
	}
	decFrom := decimalsFor(from)
	decTo := decimalsFor(to)
	majorFrom := float64(amount) / math.Pow10(decFrom)
	majorUSD := majorFrom / rateFrom
	majorTo := majorUSD * rateTo
	return int64(math.Round(majorTo * math.Pow10(decTo)))
}

// GetExchangeRates reads rates from DB and returns the FE-facing snapshot.
func (b *CommonHandler) GetExchangeRates(ctx context.Context) (commonmodel.ExchangeRateSnapshot, error) {
	rows, err := b.storage.Querier().GetExchangeRatesByBase(ctx, b.config.App.Exchange.Base)
	if err != nil {
		return commonmodel.ExchangeRateSnapshot{}, fmt.Errorf("get exchange rates: %w", err)
	}
	rates := make(map[string]float64, len(rows))
	var latest *time.Time
	for _, r := range rows {
		f, _ := r.Rate.Float64Value()
		rates[r.Target] = f.Float64
		if latest == nil || r.FetchedAt.Time.After(*latest) {
			t := r.FetchedAt.Time
			latest = &t
		}
	}
	return commonmodel.ExchangeRateSnapshot{
		Base:      b.config.App.Exchange.Base,
		Rates:     rates,
		FetchedAt: latest,
		Supported: b.config.App.Exchange.Supported,
	}, nil
}

// ConvertAmount: BE helper for cross-currency math (filter, analytics).
func (b *CommonHandler) ConvertAmount(ctx context.Context, p ConvertAmountParams) (int64, error) {
	snap, err := b.GetExchangeRates(ctx)
	if err != nil {
		return 0, err
	}
	return ConvertAmountPure(p.Amount, p.From, p.To, snap.Rates), nil
}

// IsSupportedCurrency checks against the config whitelist.
func (b *CommonHandler) IsSupportedCurrency(_ context.Context, currency string) bool {
	return slices.Contains(b.config.App.Exchange.Supported, currency)
}

// RefreshExchangeRates fetches latest rates and upserts them.
// Invoked by SetupCron on startup and on each ticker.
func (b *CommonHandler) RefreshExchangeRates(ctx context.Context) error {
	if b.exchange == nil {
		return fmt.Errorf("exchange: no provider configured")
	}
	base := b.config.App.Exchange.Base
	targets := make([]string, 0, len(b.config.App.Exchange.Supported))
	for _, c := range b.config.App.Exchange.Supported {
		if c != base {
			targets = append(targets, c)
		}
	}
	snap, err := b.exchange.FetchLatest(ctx, base, targets)
	if err != nil {
		return fmt.Errorf("refresh rates: fetch: %w", err)
	}

	for target, rate := range snap.Rates {
		raw, _ := json.Marshal(rate)
		if err := b.storage.Querier().UpsertExchangeRate(ctx, commondb.UpsertExchangeRateParams{
			Base:      base,
			Target:    target,
			Rate:      pgNumericFromFloat(rate, raw),
			FetchedAt: pgTimestamptz(snap.FetchedAt),
		}); err != nil {
			slog.Warn("upsert exchange rate failed",
				"base", base, "target", target, "err", err)
		}
	}
	return nil
}

// SetupExchangeCron starts the rate refresh goroutine. Mirrors the
// catalog search sync pattern. Safe to call once; non-blocking.
func (b *CommonHandler) SetupExchangeCron() {
	interval := b.config.App.Exchange.RefreshInterval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	go b.exchangeCronLoop(context.Background(), interval)
}

func (b *CommonHandler) exchangeCronLoop(ctx context.Context, interval time.Duration) {
	slog.Info("exchange rate cron starting", "interval", interval)
	if err := b.RefreshExchangeRates(ctx); err != nil {
		slog.Warn("initial exchange refresh failed", "err", err)
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := b.RefreshExchangeRates(ctx); err != nil {
				slog.Warn("periodic exchange refresh failed", "err", err)
			}
		}
	}
}
```

- [ ] **Step 4: Add pg helpers for numeric/timestamptz**

Also add to `internal/module/common/biz/exchange_rate.go` (or a shared helpers file if one already exists — check):

```go
import (
	"github.com/jackc/pgx/v5/pgtype"
)

func pgNumericFromFloat(v float64, raw []byte) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(string(raw))
	return n
}

func pgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
```

If the codebase already has helpers (e.g. in `internal/shared/pgsqlc/`), use those instead. Verify before adding duplicates.

- [ ] **Step 5: Add to CommonBiz interface**

In `internal/module/common/biz/interface.go`:

```go
type CommonBiz interface {
	...existing methods...

	// Exchange rates
	GetExchangeRates(ctx context.Context) (commonmodel.ExchangeRateSnapshot, error)
	ConvertAmount(ctx context.Context, params ConvertAmountParams) (int64, error)
	IsSupportedCurrency(ctx context.Context, currency string) bool
}
```

Add `exchange exchange.Client` field to `CommonHandler` struct and accept in `NewcommonBiz`:

```go
type CommonHandler struct {
	...existing fields...
	exchange exchange.Client
}

func NewcommonBiz(
	cfg *config.Config,
	storage CommonStorage,
	geocoder geocoding.Client,
	exchangeClient exchange.Client,
) (*CommonHandler, error) {
	b := &CommonHandler{
		...
		exchange: exchangeClient,
	}
	...
}
```

Regenerate Restate proxy: `go generate ./internal/module/common/biz/`

- [ ] **Step 6: Run tests**

Run: `go test ./internal/module/common/biz/... -v`
Expected: all 5 ConvertAmount tests PASS. Build must succeed.

- [ ] **Step 7: Commit**

```bash
git add internal/module/common/biz/exchange_rate.go \
        internal/module/common/biz/exchange_rate_test.go \
        internal/module/common/biz/interface.go \
        internal/module/common/biz/restate_gen.go
git commit -m "add exchange rate biz logic and cron"
```

---

### Task 6: Common transport — /currencies/rates endpoint

**Files:**
- Create: `internal/module/common/transport/echo/exchange_rate.go`
- Modify: `internal/module/common/transport/echo/common.go`

- [ ] **Step 1: Create handler**

Create `internal/module/common/transport/echo/exchange_rate.go`:

```go
package commonecho

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// GetExchangeRates returns the latest exchange rate snapshot.
// Public endpoint; no auth required.
func (h *Handler) GetExchangeRates(c echo.Context) error {
	snap, err := h.biz.GetExchangeRates(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, snap)
}
```

- [ ] **Step 2: Register route**

In `internal/module/common/transport/echo/common.go`, add to the route block:

```diff
 	api.POST("/files", h.UploadFile)
 	api.GET("/option", h.ListServiceOption)
 	api.POST("/geocode/reverse", h.ReverseGeocode)
 	api.POST("/geocode/forward", h.ForwardGeocode)
 	api.GET("/geocode/search", h.SearchGeocode)
 	api.GET("/stream", h.HandleSSE)
+	api.GET("/currencies/rates", h.GetExchangeRates)
```

- [ ] **Step 3: Build + smoke test**

Run: `make build`
Expected: no build errors.

Start dev server: `make dev` (separate terminal)
Hit endpoint: `curl http://localhost:<port>/api/v1/common/currencies/rates | jq .`
Expected: JSON with `base: "USD"`, `rates: {VND, JPY, ...}`, `supported: [...]`.

- [ ] **Step 4: Commit**

```bash
git add internal/module/common/transport/echo/
git commit -m "add /common/currencies/rates endpoint"
```

---

### Task 7: Wire exchange provider + cron into fx lifecycle

**Files:**
- Modify: `internal/module/common/fx.go`
- Modify: `internal/app/app.go`
- Modify: `config/*.yaml` (or equivalent)

- [ ] **Step 1: Add config struct**

In `config/config.go` (or wherever `App` struct is defined), add:

```go
type ExchangeConfig struct {
    Base               string        `yaml:"base" default:"USD"`
    Supported          []string      `yaml:"supported"`
    RefreshInterval    time.Duration `yaml:"refresh_interval" default:"6h"`
    HTTPTimeout        time.Duration `yaml:"http_timeout" default:"5s"`
    DefaultUserCurrency string       `yaml:"default_user_currency" default:"VND"`
    UpstreamURL        string        `yaml:"upstream_url" default:"https://api.frankfurter.dev"`
}

// Inside AppConfig:
type AppConfig struct {
    ...existing...
    Exchange ExchangeConfig `yaml:"exchange"`
}
```

- [ ] **Step 2: Add YAML defaults**

In `config/default.yaml` (or the primary config file):

```yaml
exchange:
  base: USD
  supported: [VND, USD, JPY, KRW, EUR, GBP, CNY, SGD, THB, AUD]
  refresh_interval: 6h
  http_timeout: 5s
  default_user_currency: VND
  upstream_url: https://api.frankfurter.dev
```

Find existing config file via: `ls config/*.yaml`.

- [ ] **Step 3: Wire provider in fx**

In `internal/module/common/fx.go`, add provider:

```go
func provideExchangeClient(cfg *config.Config) exchange.Client {
	return exchange.NewFrankfurter(
		cfg.App.Exchange.UpstreamURL,
		&http.Client{Timeout: cfg.App.Exchange.HTTPTimeout},
	)
}

// In the fx.Options block:
fx.Provide(provideExchangeClient),
```

Ensure `NewcommonBiz` signature change from Task 5 is compatible — fx should auto-inject `exchange.Client`.

- [ ] **Step 4: Start cron from app lifecycle**

In `internal/app/app.go`, find where `catalog.SetupCron()` or similar is called on startup. Add alongside:

```go
// Start exchange rate refresh cron
commonHandler.SetupExchangeCron()
```

If no such pattern exists, add to the `fx.Lifecycle` OnStart hook in `common/fx.go`.

- [ ] **Step 5: Run server + verify cron**

Start: `make dev`
Expected log within 1s: "exchange rate cron starting interval=6h0m0s" and either success or a network-fail warning.

Hit endpoint: `curl http://localhost:<port>/api/v1/common/currencies/rates | jq .fetched_at`
Expected: timestamp close to now (if upstream reachable) or epoch (if upstream unreachable).

- [ ] **Step 6: Commit**

```bash
git add internal/module/common/fx.go internal/app/app.go config/
git commit -m "wire exchange provider and cron into app lifecycle"
```

---

### Task 8: Custom iso4217 validator

**Files:**
- Create: `internal/shared/validator/iso4217.go`

- [ ] **Step 1: Find existing validator setup**

Run: `grep -rn "validator.New\|RegisterValidation" internal/shared/validator/`
Identify where custom validators are registered.

- [ ] **Step 2: Implement validator**

Create `internal/shared/validator/iso4217.go`:

```go
package validator

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

var iso4217Re = regexp.MustCompile(`^[A-Z]{3}$`)

// Iso4217 validates that a field is a 3-letter uppercase ISO 4217 code.
// Whitelist validation against config.Exchange.Supported is performed
// separately at the biz layer (this validator is format-only).
func Iso4217(fl validator.FieldLevel) bool {
	return iso4217Re.MatchString(fl.Field().String())
}
```

- [ ] **Step 3: Register validator**

In the existing validator bootstrap (wherever other custom validators are registered), add:

```go
_ = validate.RegisterValidation("iso4217", Iso4217)
```

- [ ] **Step 4: Build + test**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/shared/validator/
git commit -m "add iso4217 custom validator"
```

---

### Task 9: Account profile settings — model + biz

**Files:**
- Modify: `internal/module/account/model/account.go`
- Modify: `internal/module/account/biz/profile.go`
- Modify: `internal/module/account/biz/interface.go`
- Modify: `internal/module/account/biz/auth.go`
- Create: `internal/module/account/biz/profile_settings_test.go`

- [ ] **Step 1: Add ProfileSettings to model**

In `internal/module/account/model/account.go`:

```go
// ProfileSettings is a typed view of account.profile.settings JSONB.
// Unknown fields in DB are preserved across updates via load-merge-write.
type ProfileSettings struct {
	PreferredCurrency string `json:"preferred_currency,omitempty"`
}

type Profile struct {
	ID          uuid.UUID `json:"id"`
	...existing fields...
	Description null.String     `json:"description"`
	Settings    ProfileSettings `json:"settings"`
}
```

- [ ] **Step 2: Add error sentinel**

In `internal/module/account/model/error.go`:

```go
var ErrUnsupportedCurrency = errors.New("account: unsupported currency")
```

- [ ] **Step 3: Add interface method**

In `internal/module/account/biz/interface.go`:

```go
type AccountBiz interface {
	...existing...
	UpdateProfileSettings(ctx context.Context, params UpdateProfileSettingsParams) (accountmodel.ProfileSettings, error)
}
```

- [ ] **Step 4: Write failing biz tests**

Create `internal/module/account/biz/profile_settings_test.go`:

```go
package accountbiz_test

import (
	"encoding/json"
	"testing"

	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
)

// Verify unknown JSONB keys are preserved after patch.
func TestMergeSettings_PreservesUnknownKeys(t *testing.T) {
	existing := json.RawMessage(`{"preferred_currency":"VND","theme":"dark"}`)
	merged, err := accountbiz.MergeSettings(existing, accountmodel.ProfileSettings{
		PreferredCurrency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(merged, &got)
	if got["preferred_currency"] != "USD" {
		t.Errorf("preferred_currency = %v, want USD", got["preferred_currency"])
	}
	if got["theme"] != "dark" {
		t.Errorf("theme should be preserved, got %v", got["theme"])
	}
}

// Empty existing → typed fields only.
func TestMergeSettings_EmptyExisting(t *testing.T) {
	merged, err := accountbiz.MergeSettings(nil, accountmodel.ProfileSettings{
		PreferredCurrency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(merged, &got)
	if got["preferred_currency"] != "USD" {
		t.Errorf("preferred_currency = %v, want USD", got["preferred_currency"])
	}
}
```

Expected fail: `undefined: accountbiz.MergeSettings`

- [ ] **Step 5: Implement merge helper + UpdateProfileSettings**

In `internal/module/account/biz/profile.go`, add at the bottom:

```go
// MergeSettings overlays typed fields from patch on top of existing
// JSONB, preserving unknown keys. Exported for direct testing.
func MergeSettings(existing json.RawMessage, patch accountmodel.ProfileSettings) (json.RawMessage, error) {
	merged := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &merged); err != nil {
			merged = map[string]any{} // tolerate invalid; start fresh
		}
	}
	if patch.PreferredCurrency != "" {
		merged["preferred_currency"] = patch.PreferredCurrency
	}
	return json.Marshal(merged)
}

type UpdateProfileSettingsParams struct {
	Issuer            accountmodel.AuthenticatedAccount
	AccountID         uuid.UUID   `validate:"required"`
	PreferredCurrency null.String `validate:"omitempty,iso4217"`
}

func (b *AccountHandler) UpdateProfileSettings(
	ctx restate.Context, params UpdateProfileSettingsParams,
) (accountmodel.ProfileSettings, error) {
	var zero accountmodel.ProfileSettings
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("update profile settings", err)
	}
	if params.Issuer.ID != params.AccountID {
		return zero, sharedmodel.ErrForbidden
	}

	patch := accountmodel.ProfileSettings{}
	if params.PreferredCurrency.Valid {
		code := strings.ToUpper(params.PreferredCurrency.String)
		if !b.common.IsSupportedCurrency(ctx, code) {
			return zero, accountmodel.ErrUnsupportedCurrency
		}
		patch.PreferredCurrency = code
	}

	prof, err := b.storage.Querier().GetProfile(ctx, accountdb.GetProfileParams{
		ID: uuid.NullUUID{UUID: params.AccountID, Valid: true},
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("update profile settings", err)
	}

	merged, err := MergeSettings(prof.Settings, patch)
	if err != nil {
		return zero, sharedmodel.WrapErr("update profile settings", err)
	}

	updated, err := b.storage.Querier().UpdateProfileSettings(ctx, accountdb.UpdateProfileSettingsParams{
		ID:       params.AccountID,
		Settings: merged,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("update profile settings", err)
	}

	var out accountmodel.ProfileSettings
	_ = json.Unmarshal(updated.Settings, &out)
	return out, nil
}
```

Add imports: `encoding/json`, `strings`.

- [ ] **Step 6: Update `dbToProfile` to parse settings**

In `internal/module/account/biz/profile.go` `dbToProfile` function, after existing field mapping:

```go
	var settings accountmodel.ProfileSettings
	if len(profile.Settings) > 0 {
		_ = json.Unmarshal(profile.Settings, &settings) // tolerate invalid
	}

	return accountmodel.Profile{
		...existing fields...
		Settings: settings,
	}
```

- [ ] **Step 7: Update Register flow**

In `internal/module/account/biz/auth.go`, find where `UpsertProfile` / profile creation occurs. Ensure it seeds `settings`:

```go
defaultCurrency := b.common.Config().App.Exchange.DefaultUserCurrency
// or access via b.config directly if available
seedSettings, _ := json.Marshal(accountmodel.ProfileSettings{
    PreferredCurrency: defaultCurrency,
})
// Pass seedSettings into the InsertProfile call's Settings arg.
```

If the common biz doesn't expose Config, use `b.config.App.Exchange.DefaultUserCurrency` directly (AccountHandler already holds config per line 94 of interface.go).

The DB default is `{"preferred_currency": "VND"}` which is a safety net — explicit seeding ensures future config changes propagate.

- [ ] **Step 8: Regenerate Restate proxy**

Run: `go generate ./internal/module/account/biz/`

- [ ] **Step 9: Run tests + build**

Run: `go test ./internal/module/account/biz/... -v`
Expected: both MergeSettings tests PASS; build succeeds.

- [ ] **Step 10: Commit**

```bash
git add internal/module/account/
git commit -m "add UpdateProfileSettings biz method and merge helper"
```

---

### Task 10: Account transport — PATCH /me/settings

**Files:**
- Modify: `internal/module/account/transport/echo/me.go`

- [ ] **Step 1: Add handler**

In `internal/module/account/transport/echo/me.go`, add:

```go
type updateSettingsRequest struct {
	PreferredCurrency *string `json:"preferred_currency"`
}

// UpdateMeSettings handles PATCH /account/me/settings.
// Only the authenticated user can modify their own settings.
func (h *Handler) UpdateMeSettings(c echo.Context) error {
	issuer, err := h.authzIssuer(c) // reuse existing JWT extraction helper
	if err != nil {
		return err
	}
	var req updateSettingsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	params := accountbiz.UpdateProfileSettingsParams{
		Issuer:    issuer,
		AccountID: issuer.ID,
	}
	if req.PreferredCurrency != nil {
		params.PreferredCurrency = null.StringFrom(*req.PreferredCurrency)
	}

	settings, err := h.biz.UpdateProfileSettings(c.Request().Context(), params)
	if err != nil {
		if errors.Is(err, accountmodel.ErrUnsupportedCurrency) {
			return echo.NewHTTPError(http.StatusBadRequest, "unsupported currency")
		}
		if errors.Is(err, sharedmodel.ErrForbidden) {
			return echo.NewHTTPError(http.StatusForbidden, "forbidden")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, settings)
}
```

Look at existing handlers in the same file for the correct auth-extraction helper name.

- [ ] **Step 2: Register route**

Find the existing `me` route block (likely `api.GET("/me", ...)` and `api.PATCH("/me", ...)`). Add:

```go
api.PATCH("/me/settings", h.UpdateMeSettings)
```

- [ ] **Step 3: Smoke test with curl**

```bash
# Get a JWT first via /auth/login, export as $TOKEN

curl -X PATCH http://localhost:8080/api/v1/account/me/settings \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"preferred_currency":"USD"}'
```

Expected: `{"preferred_currency":"USD"}` response.

Invalid currency test:
```bash
curl -X PATCH http://localhost:8080/api/v1/account/me/settings \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"preferred_currency":"XYZ"}'
```
Expected: 400 "unsupported currency".

- [ ] **Step 4: Commit**

```bash
git add internal/module/account/transport/echo/me.go
git commit -m "add PATCH /me/settings endpoint"
```

---

### Task 11: Fix compilation after dropping account.account.settings

**Files:** varies — compiler-driven

- [ ] **Step 1: Run full build, list errors**

Run: `go build ./... 2>&1 | head -40`

Any remaining reference to `AccountAccount.Settings` must be fixed.

- [ ] **Step 2: Fix each error**

Typical sites:
- `internal/module/account/biz/auth.go` — Register flow might have set `Settings: []byte("{}")` in `CreateCopyAccount` params; remove that field.
- `accountdb/sqlc/copyfrom.go` — auto-regenerated, should already be clean.

For each compile error, open the file and remove the `Settings` reference. Do NOT add defensive code — just delete.

- [ ] **Step 3: Build clean**

Run: `go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "remove account.account.settings references"
```

---

## Phase 2 — Frontend Foundation (parallelizable)

### Task 12: lib/money.ts utilities

**Files:**
- Create: `lib/money.ts`
- Create: `lib/money.test.ts`

Working dir: `../shopnexus-website-v3/`

- [ ] **Step 1: Write failing tests**

Create `lib/money.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import { convertMoney, formatMoney, formatPriceInline } from "./money"

describe("formatMoney", () => {
  it("formats VND with no decimals", () => {
    expect(formatMoney(299000, "VND", "vi-VN")).toMatch(/299[.,]000\s*₫|₫299[.,]000/)
  })
  it("formats USD with 2 decimals (cents input)", () => {
    expect(formatMoney(1250, "USD", "en-US")).toBe("$12.50")
  })
  it("formats JPY with no decimals", () => {
    expect(formatMoney(10000, "JPY", "ja-JP")).toMatch(/¥\s?10[.,]000/)
  })
})

describe("convertMoney", () => {
  const rates = { VND: 25000, JPY: 155, EUR: 0.92 }

  it("returns identity for same currency", () => {
    expect(convertMoney(100000, "VND", "VND", rates)).toBe(100000)
  })
  it("USD→VND scales correctly", () => {
    // $10.00 (1000 cents) → 10 * 25000 = 250000 VND
    expect(convertMoney(1000, "USD", "VND", rates)).toBe(250000)
  })
  it("VND→USD scales correctly", () => {
    // 250000 VND → 10 USD → 1000 cents
    expect(convertMoney(250000, "VND", "USD", rates)).toBe(1000)
  })
  it("returns original when rate missing", () => {
    expect(convertMoney(100000, "VND", "XYZ", rates)).toBe(100000)
  })
})

describe("formatPriceInline", () => {
  const rates = { VND: 25000 }
  it("returns single format when same currency", () => {
    expect(formatPriceInline(299000, "VND", "VND", rates)).toMatch(/299[.,]000\s*₫|₫299[.,]000/)
  })
  it("returns both when currencies differ", () => {
    const s = formatPriceInline(1000, "USD", "VND", rates)
    expect(s).toContain("$12.50")
    expect(s).toMatch(/250[.,]000/)
    expect(s).toContain("≈")
  })
  it("gracefully returns native when rates undefined", () => {
    expect(formatPriceInline(1000, "USD", "VND", undefined)).toBe("$12.50")
  })
})
```

- [ ] **Step 2: Run tests — fail**

Run: `bunx vitest run lib/money.test.ts`
Expected: "Cannot find module './money'"

- [ ] **Step 3: Implement money.ts**

Create `lib/money.ts`:

```ts
// Smallest-unit-aware money utilities. Zero external deps — uses Intl APIs.
// All amounts are integers in the smallest unit of their currency
// (e.g. 299000 = ₫299,000; 1250 = $12.50).

const DECIMALS: Record<string, number> = {
  VND: 0, USD: 2, JPY: 0, KRW: 0, EUR: 2,
  GBP: 2, CNY: 2, SGD: 2, THB: 2, AUD: 2,
}

const getDecimals = (currency: string): number =>
  DECIMALS[currency] ?? 2

/**
 * Format a smallest-unit integer as a localized currency string.
 * `locale` defaults to the runtime's locale.
 */
export function formatMoney(
  amount: number,
  currency: string,
  locale?: string,
): string {
  const decimals = getDecimals(currency)
  const major = amount / Math.pow(10, decimals)
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency,
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(major)
}

/**
 * Convert an amount between currencies using USD-based rates. Returns
 * integer smallest-unit in the target currency. Returns original unchanged
 * if either rate is missing (fail-open: display native rather than lying).
 */
export function convertMoney(
  amount: number,
  from: string,
  to: string,
  ratesFromUSD: Record<string, number>,
): number {
  if (from === to) return amount

  const rateFrom = from === "USD" ? 1 : ratesFromUSD[from]
  const rateTo = to === "USD" ? 1 : ratesFromUSD[to]
  if (!rateFrom || !rateTo) return amount

  const decFrom = getDecimals(from)
  const decTo = getDecimals(to)

  const majorFrom = amount / Math.pow(10, decFrom)
  const majorUSD = majorFrom / rateFrom
  const majorTo = majorUSD * rateTo
  return Math.round(majorTo * Math.pow(10, decTo))
}

/**
 * Localized currency display name, e.g. "US Dollar" / "Đô la Mỹ".
 */
export function getCurrencyName(currency: string, locale?: string): string {
  try {
    return (
      new Intl.DisplayNames([locale ?? "en"], { type: "currency" }).of(currency) ??
      currency
    )
  } catch {
    return currency
  }
}

/**
 * Inline string formatter for non-JSX contexts (toasts, button labels).
 * Mirrors <Price> behavior but returns plain text. When currencies differ
 * and rates are available, returns "$X (≈ ₫Y)".
 */
export function formatPriceInline(
  amount: number,
  currency: string,
  preferred: string,
  rates: Record<string, number> | undefined,
): string {
  const native = formatMoney(amount, currency)
  if (!rates || currency === preferred) return native
  const converted = convertMoney(amount, currency, preferred, rates)
  return `${native} (≈ ${formatMoney(converted, preferred)})`
}
```

- [ ] **Step 4: Run tests — pass**

Run: `bunx vitest run lib/money.test.ts`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add lib/money.ts lib/money.test.ts
git commit -m "add money utilities (format, convert, inline)"
```

---

### Task 13: core/common/currency.ts — hooks

**Files:**
- Create: `core/common/currency.ts`

- [ ] **Step 1: Implement hooks**

Create `core/common/currency.ts`:

```ts
import { useMutation, useQuery } from "@tanstack/react-query"
import { getQueryClient } from "@/lib/queryclient/query-client"
import { customFetchStandard } from "@/lib/queryclient/custom-fetch"
import { useGetMe } from "@/core/account/account"

export type ExchangeRateSnapshot = {
  base: string
  rates: Record<string, number>
  fetched_at: string | null
  supported: string[]
}

const RATES_QUERY_KEY = ["common", "exchange-rates"] as const

export const useExchangeRates = () =>
  useQuery({
    queryKey: RATES_QUERY_KEY,
    queryFn: () =>
      customFetchStandard<ExchangeRateSnapshot>("common/currencies/rates"),
    staleTime: 60 * 60 * 1000,           // 1h
    gcTime: 24 * 60 * 60 * 1000,
    refetchOnWindowFocus: false,
  })

const LS_KEY = "preferred_currency"
const DEFAULT_CURRENCY = "VND"

/**
 * Returns the active preferred currency. Fallback chain:
 *   profile.settings.preferred_currency > localStorage > "VND"
 */
export function usePreferredCurrency(): string {
  const { data: me } = useGetMe()
  const fromProfile = me?.settings?.preferred_currency
  if (fromProfile) return fromProfile

  if (typeof window !== "undefined") {
    const fromLocal = window.localStorage.getItem(LS_KEY)
    if (fromLocal) return fromLocal
  }
  return DEFAULT_CURRENCY
}

export const useUpdatePreferredCurrency = () => {
  const queryClient = getQueryClient()
  return useMutation({
    mutationFn: (currency: string) =>
      customFetchStandard("account/me/settings", {
        method: "PATCH",
        body: JSON.stringify({ preferred_currency: currency }),
      }),
    onSuccess: (_, currency) => {
      if (typeof window !== "undefined") {
        window.localStorage.setItem(LS_KEY, currency)
      }
      queryClient.invalidateQueries({ queryKey: ["account", "me"] })
    },
  })
}
```

- [ ] **Step 2: Build check**

Run: `bun run build` (or `bunx tsc --noEmit`)
Expected: no errors (module resolves).

- [ ] **Step 3: Commit**

```bash
git add core/common/currency.ts
git commit -m "add currency hooks (rates, preferred, update)"
```

---

### Task 14: Update AccountProfile type with settings

**Files:**
- Modify: `core/account/account.ts`

- [ ] **Step 1: Add type + field**

In `core/account/account.ts`, around the `AccountProfile` type:

```ts
export type ProfileSettings = {
  preferred_currency?: string
}

export type AccountProfile = {
  id: string
  date_created: string
  date_updated: string
  ...existing...
  description: string | null
  settings: ProfileSettings  // NEW
}
```

- [ ] **Step 2: Build check**

Run: `bunx tsc --noEmit`
Expected: no errors (usePreferredCurrency from Task 13 now resolves profile.settings).

- [ ] **Step 3: Commit**

```bash
git add core/account/account.ts
git commit -m "add settings field to AccountProfile type"
```

---

### Task 15: <Price> component

**Files:**
- Create: `components/ui/price.tsx`
- Create: `components/ui/price.test.tsx`

- [ ] **Step 1: Write failing tests**

Create `components/ui/price.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { describe, expect, it, vi } from "vitest"
import { Price } from "./price"

// Mock hooks
vi.mock("@/core/common/currency", () => ({
  usePreferredCurrency: () => "VND",
  useExchangeRates: () => ({
    data: { base: "USD", rates: { VND: 25000, USD: 1 }, fetched_at: null, supported: [] },
  }),
}))

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient()
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
}

describe("<Price>", () => {
  it("shows single line when currency matches preferred", () => {
    render(wrap(<Price amount={299000} currency="VND" />))
    expect(screen.getByText(/299[.,]000/)).toBeInTheDocument()
    expect(screen.queryByText(/≈/)).toBeNull()
  })

  it("shows both with preferred primary when currencies differ", () => {
    render(wrap(<Price amount={1000} currency="USD" emphasis="preferred" />))
    expect(screen.getByText(/250[.,]000/)).toBeInTheDocument() // converted VND primary
    expect(screen.getByText(/≈.*\$?12\.50/)).toBeInTheDocument() // native USD secondary
  })

  it("hides converted when hideConverted=true", () => {
    render(wrap(<Price amount={1000} currency="USD" hideConverted />))
    expect(screen.queryByText(/≈/)).toBeNull()
  })

  it("native-only emphasis shows only native regardless of currencies", () => {
    render(wrap(<Price amount={1000} currency="USD" emphasis="native-only" />))
    expect(screen.queryByText(/≈/)).toBeNull()
    expect(screen.getByText(/\$12\.50/)).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Implement Price**

Create `components/ui/price.tsx`:

```tsx
"use client"

import { useExchangeRates, usePreferredCurrency } from "@/core/common/currency"
import { convertMoney, formatMoney } from "@/lib/money"
import { cn } from "@/lib/utils"

type Emphasis = "preferred" | "native" | "native-only"

export type PriceProps = {
  /** smallest-unit integer in `currency` */
  amount: number
  /** ISO 4217 code of `amount` */
  currency: string
  /** Which side is visually dominant. Default "preferred". */
  emphasis?: Emphasis
  /** Force-hide converted line (e.g. per-line items when header already shows both) */
  hideConverted?: boolean
  /** Append "at current rate" hint under converted line */
  showRateHint?: boolean
  className?: string
}

export function Price({
  amount,
  currency,
  emphasis = "preferred",
  hideConverted = false,
  showRateHint = false,
  className,
}: PriceProps) {
  const preferred = usePreferredCurrency()
  const { data: rateData } = useExchangeRates()

  const sameCurrency = currency === preferred
  const ratesReady = !!rateData
  const showBoth =
    !sameCurrency && ratesReady && !hideConverted && emphasis !== "native-only"

  const native = formatMoney(amount, currency)
  const converted = showBoth
    ? formatMoney(convertMoney(amount, currency, preferred, rateData.rates), preferred)
    : null

  if (!showBoth) {
    return <span className={className}>{native}</span>
  }

  const primary = emphasis === "preferred" ? converted : native
  const secondary = emphasis === "preferred" ? native : converted

  return (
    <span className={cn("inline-flex flex-col leading-tight", className)}>
      <span className="font-semibold">{primary}</span>
      <span className="text-xs text-muted-foreground font-normal">
        ≈ {secondary}
        {showRateHint && <span className="ml-1 opacity-60"> at current rate</span>}
      </span>
    </span>
  )
}
```

- [ ] **Step 3: Run tests — pass**

Run: `bunx vitest run components/ui/price.test.tsx`
Expected: 4 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add components/ui/price.tsx components/ui/price.test.tsx
git commit -m "add <Price> component with emphasis rules"
```

---

### Task 16: CurrencyPicker component

**Files:**
- Create: `components/ui/currency-picker.tsx`

- [ ] **Step 1: Install flag icons**

Run (in `shopnexus-website-v3`):
```bash
bun add country-flag-icons
```

- [ ] **Step 2: Implement CurrencyPicker**

Create `components/ui/currency-picker.tsx`:

```tsx
"use client"

import { Check, ChevronsUpDown } from "lucide-react"
import { useState } from "react"
import { getCurrencyName } from "@/lib/money"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

// Map ISO 4217 currency -> primary ISO 3166-1 alpha-2 country for flag.
const CURRENCY_TO_COUNTRY: Record<string, string> = {
  VND: "VN", USD: "US", JPY: "JP", KRW: "KR", EUR: "EU",
  GBP: "GB", CNY: "CN", SGD: "SG", THB: "TH", AUD: "AU",
}
const POPULAR = ["VND", "USD", "JPY", "KRW", "EUR"]

function FlagEmoji({ country }: { country: string }) {
  // Emoji flag via regional indicators. EU has no emoji flag — fall back to 🇪🇺.
  if (country === "EU") return <span aria-hidden="true">🇪🇺</span>
  if (country.length !== 2) return <span aria-hidden="true">💱</span>
  const base = 127397
  const chars = [...country.toUpperCase()].map((c) => String.fromCodePoint(base + c.charCodeAt(0)))
  return <span aria-hidden="true">{chars.join("")}</span>
}

export type CurrencyPickerProps = {
  value: string
  supported: string[]
  onChange: (currency: string) => void
  disabled?: boolean
}

export function CurrencyPicker({
  value, supported, onChange, disabled,
}: CurrencyPickerProps) {
  const [open, setOpen] = useState(false)
  const popular = supported.filter((c) => POPULAR.includes(c))
  const rest = supported.filter((c) => !POPULAR.includes(c)).sort()

  const render = (c: string) => (
    <CommandItem
      key={c}
      value={c}
      onSelect={() => {
        onChange(c)
        setOpen(false)
      }}
    >
      <FlagEmoji country={CURRENCY_TO_COUNTRY[c] ?? "UN"} />
      <span className="ml-2 font-medium">{c}</span>
      <span className="ml-2 text-muted-foreground truncate">
        {getCurrencyName(c)}
      </span>
      {value === c && <Check className="ml-auto h-4 w-4" />}
    </CommandItem>
  )

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          disabled={disabled}
          className={cn("w-full justify-between")}
        >
          <span className="flex items-center gap-2">
            <FlagEmoji country={CURRENCY_TO_COUNTRY[value] ?? "UN"} />
            {value} — {getCurrencyName(value)}
          </span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="p-0" align="start">
        <Command>
          <CommandInput placeholder="Search currency..." />
          <CommandList>
            <CommandEmpty>No currency found.</CommandEmpty>
            {popular.length > 0 && (
              <CommandGroup heading="Popular">{popular.map(render)}</CommandGroup>
            )}
            {rest.length > 0 && (
              <>
                <CommandSeparator />
                <CommandGroup heading="All currencies">{rest.map(render)}</CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
```

- [ ] **Step 3: Build check**

Run: `bunx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add components/ui/currency-picker.tsx package.json bun.lock
git commit -m "add currency picker with flag icons"
```

---

## Phase 3 — FE Migration (parallelizable by file group)

Each of the following tasks (17–26) is **independent** — subagents can run them in parallel. All depend only on Phase 1 + Phase 2 completion.

### Task 17: Settings page — wire currency picker

**Files:**
- Modify: `app/(marketplace)/account/settings/_components/preferences-card.tsx`

- [ ] **Step 1: Replace hardcoded currency line with CurrencyPicker**

Open `app/(marketplace)/account/settings/_components/preferences-card.tsx` and find the block around lines 47–53 showing "Currency" / "USD ($)". Replace with:

```tsx
import { CurrencyPicker } from "@/components/ui/currency-picker"
import {
  useExchangeRates,
  usePreferredCurrency,
  useUpdatePreferredCurrency,
} from "@/core/common/currency"
import { toast } from "sonner"

// ... inside component:
const preferred = usePreferredCurrency()
const { data: rates } = useExchangeRates()
const updatePreferred = useUpdatePreferredCurrency()

// Replace the "Currency / USD ($)" rendering block with:
<div className="flex items-center justify-between gap-4">
  <div>
    <p className="font-medium">Currency</p>
    <p className="text-sm text-muted-foreground">
      Used to display prices across the site
    </p>
  </div>
  <div className="w-64">
    {rates && (
      <CurrencyPicker
        value={preferred}
        supported={rates.supported}
        onChange={(c) =>
          updatePreferred.mutate(c, {
            onSuccess: () => toast.success(`Now showing prices in ${c}`),
            onError:  () => toast.error("Failed to update currency"),
          })
        }
        disabled={updatePreferred.isPending}
      />
    )}
  </div>
</div>
```

- [ ] **Step 2: Manual verify in browser**

Start dev server if not running: `bun dev`
Navigate to `/account/settings`. Verify:
- Currency picker renders with current value
- Opening picker shows grouped list (Popular + All)
- Selecting a new currency shows toast + picker updates

- [ ] **Step 3: Commit**

```bash
git add app/(marketplace)/account/settings/_components/preferences-card.tsx
git commit -m "wire currency picker into preferences"
```

---

### Task 18: Product card migration

**Files:**
- Modify: `components/product/product-card.tsx`

- [ ] **Step 1: Replace formatPrice with <Price>**

In `components/product/product-card.tsx`:

```tsx
import { Price } from "@/components/ui/price"
// Remove: import { formatPrice, ... } from "@/lib/utils"
// Keep:   import { formatSoldCount, cn } from "@/lib/utils"
```

Replace line 122:
```diff
-{formatPrice(product.price, product.currency)}
+<Price amount={product.price} currency={product.currency} emphasis="preferred" />
```

Replace line 126 (strikethrough original):
```diff
-<span className="text-sm text-muted-foreground line-through">
-  {formatPrice(product.original_price, product.currency)}
-</span>
+<Price
+  amount={product.original_price}
+  currency={product.currency}
+  emphasis="preferred"
+  hideConverted
+  className="text-sm text-muted-foreground line-through"
+/>
```

Replace line 241:
```diff
-{formatPrice(product.price, product.currency)}
+<Price amount={product.price} currency={product.currency} emphasis="preferred" />
```

- [ ] **Step 2: Manual verify**

Navigate to any product listing page. Verify:
- VND-only products (if preferred=VND): single-line price
- Non-VND products (or after switching preferred to USD): two-line display
- Strikethrough original price shows hideConverted single-line

- [ ] **Step 3: Commit**

```bash
git add components/product/product-card.tsx
git commit -m "migrate product card to <Price>"
```

---

### Task 19: Product detail page migration

**Files:**
- Modify: `app/(marketplace)/product/[slug]/page.tsx`

- [ ] **Step 1: Imports**

Replace:
```diff
-import { formatPrice, formatSoldCount } from "@/lib/utils"
+import { formatSoldCount } from "@/lib/utils"
+import { formatPriceInline } from "@/lib/money"
+import { Price } from "@/components/ui/price"
+import { useExchangeRates, usePreferredCurrency } from "@/core/common/currency"
```

Add hooks at top of component body:
```tsx
const preferred = usePreferredCurrency()
const { data: rateData } = useExchangeRates()
```

- [ ] **Step 2: Replace L310 (toast template)**

```diff
-description: `${product?.name} x${quantity} - ${formatPrice(selectedSku.price * quantity, product?.currency)}`,
+description: `${product?.name} x${quantity} - ${formatPriceInline(
+  selectedSku.price * quantity,
+  product?.currency ?? "VND",
+  preferred,
+  rateData?.rates,
+)}`,
```

- [ ] **Step 3: Replace L515–517 (selected SKU / price range)**

```diff
-<span className="...">
-  {selectedSku
-    ? formatPrice(selectedSku.price, product?.currency)
-    : priceRange
-      ? `${formatPrice(priceRange.min, product?.currency)} – ${formatPrice(priceRange.max, product?.currency)}`
-      : null}
-</span>
+{selectedSku ? (
+  <Price amount={selectedSku.price} currency={product?.currency ?? "VND"} emphasis="preferred" />
+) : priceRange ? (
+  <span className="flex items-center gap-1">
+    <Price amount={priceRange.min} currency={product?.currency ?? "VND"} emphasis="preferred" />
+    <span>–</span>
+    <Price amount={priceRange.max} currency={product?.currency ?? "VND"} emphasis="preferred" />
+  </span>
+) : null}
```

- [ ] **Step 4: Replace L523, L531 (strikethrough original + other price)**

Each `formatPrice(x, product?.currency)` call becomes:
```tsx
<Price amount={x} currency={product?.currency ?? "VND"} emphasis="preferred"
       hideConverted={/* for strikethrough */} />
```

For L523 (original/strikethrough): use `hideConverted`.
For L531: normal two-line.

- [ ] **Step 5: Replace L1056, L1067, L1079 (sticky buy bar)**

Same pattern — `<Price>` with `emphasis="preferred"`. L1079 is total qty × price → compute amount inline, pass to `<Price>`.

- [ ] **Step 6: Manual verify**

Load a product page (ideally one with non-VND currency if available; otherwise change preferred to USD to trigger conversion).
Verify all 8 price locations render correctly. Add to cart → verify toast shows both currencies.

- [ ] **Step 7: Commit**

```bash
git add app/(marketplace)/product/[slug]/page.tsx
git commit -m "migrate product detail page to <Price>"
```

---

### Task 20: Cart sheet + cart page — mixed currency grouping

**Files:**
- Modify: `components/cart/cart-sheet.tsx`
- Modify: `app/(marketplace)/cart/page.tsx`

- [ ] **Step 1: Cart sheet — per-line <Price>**

In `components/cart/cart-sheet.tsx`:

```diff
-import { formatPrice } from "@/lib/utils"
+import { Price } from "@/components/ui/price"
+import { usePreferredCurrency, useExchangeRates } from "@/core/common/currency"
+import { convertMoney } from "@/lib/money"
+import groupBy from "lodash/groupBy"
```

Replace L108 (per-item price):
```diff
-{formatPrice(item.sku.price)}
+<Price amount={item.sku.price} currency={item.sku.spu.currency} emphasis="preferred" hideConverted />
```

**Note:** If `item.sku.spu.currency` is not available on the cart item shape, check the cart type in `core/order/order.buyer.ts`. If missing, add `currency` to the cart item type and ensure backend includes it (should already — SPU has currency column).

- [ ] **Step 2: Cart sheet — grouped total (L155)**

Replace the single-line total with grouped subtotals + grand total:

```tsx
const preferred = usePreferredCurrency()
const { data: rateData } = useExchangeRates()
const groups = groupBy(items, (i) => i.sku.spu.currency)
const currencies = Object.keys(groups)

// ... replace the <span className="font-semibold">{formatPrice(total)}</span> block:
<div className="flex flex-col items-end gap-1">
  {currencies.map((c) => {
    const sub = groups[c].reduce((s, i) => s + i.sku.price * i.quantity, 0)
    return <Price key={c} amount={sub} currency={c} emphasis="preferred" className="font-semibold" />
  })}
  {currencies.length > 1 && rateData && (
    <span className="text-xs text-muted-foreground">
      Grand total ≈ {formatMoney(
        currencies.reduce((sum, c) => {
          const sub = groups[c].reduce((s, i) => s + i.sku.price * i.quantity, 0)
          return sum + convertMoney(sub, c, preferred, rateData.rates)
        }, 0),
        preferred,
      )}
    </span>
  )}
</div>
```

Import: `import { formatMoney } from "@/lib/money"`.

- [ ] **Step 3: Cart page — same pattern**

In `app/(marketplace)/cart/page.tsx`, apply identical grouping logic for lines 125, 129, 198, 209. Per-item rows use `<Price emphasis="preferred" hideConverted>`; subtotal (L198, L209) uses grouped rendering.

- [ ] **Step 4: Manual verify**

Add a single-currency cart → single subtotal line. Add items from two sellers with different currencies (if available in seed data — if not, seed one manually via backend or modify a product's currency temporarily).

Expected:
- Per-item prices in native
- Grouped subtotals (₫150,000 / ¥5,000)
- Grand total "≈ $X" below in muted text

- [ ] **Step 5: Commit**

```bash
git add components/cart/cart-sheet.tsx app/(marketplace)/cart/page.tsx
git commit -m "migrate cart to <Price> with mixed-currency grouping"
```

---

### Task 21: Checkout page migration

**Files:**
- Modify: `app/(marketplace)/checkout/page.tsx`

**Emphasis rule:** `native` (user actually pays in native currency).

- [ ] **Step 1: Imports**

```diff
-import { formatPrice } from "@/lib/utils"
+import { Price } from "@/components/ui/price"
+import { formatPriceInline } from "@/lib/money"
+import { usePreferredCurrency, useExchangeRates } from "@/core/common/currency"
```

Hooks in component:
```tsx
const preferred = usePreferredCurrency()
const { data: rateData } = useExchangeRates()
```

- [ ] **Step 2: Replace each formatPrice call**

For each line (325, 431, 436, 495, 512, 516, 521, 527):

Item prices (325, 495) — use item's native currency with `emphasis="native"`:
```tsx
<Price amount={item.sku.price * item.quantity} currency={item.sku.spu.currency} emphasis="native" />
```

Wallet-related (431, 436) — always VND, wallet is VND:
```tsx
<Price amount={walletBalance} currency="VND" emphasis="native" hideConverted={preferred === "VND"} />
```

Subtotal/total (512, 527) — if single-currency cart, use that currency; if mixed, render grouped:
```tsx
// Same grouping approach as cart page
```

Shipping (516) — keep "Calculated at confirmation" branch; the number branch uses `<Price>` with cart's currency.

Wallet deduction (521) — VND:
```tsx
<Price amount={walletDeduction} currency="VND" emphasis="native" hideConverted={preferred === "VND"} />
```

- [ ] **Step 3: Replace Pay button (L456, L544)**

```diff
-`Pay ${formatPrice(estimatedTotal)}`
+`Pay ${formatPriceInline(estimatedTotal, cartCurrency, preferred, rateData?.rates)}`
```

`cartCurrency` is the primary cart currency (first non-empty). For mixed cart, button shows "Pay ₫X (≈ $Y)".

- [ ] **Step 4: Add "You will be charged in X" hint**

Above the Pay button:
```tsx
{cartCurrencies.length === 1 && cartCurrencies[0] !== preferred && (
  <p className="text-xs text-muted-foreground">
    You will be charged in {cartCurrencies[0]} ({getCurrencyName(cartCurrencies[0])})
  </p>
)}
```

Import: `import { getCurrencyName } from "@/lib/money"`.

- [ ] **Step 5: Manual verify**

Navigate to checkout with items. Verify:
- Line items show native currency, muted converted below
- Wallet shows VND + converted (if preferred ≠ VND)
- Pay button shows native prominently
- "You will be charged in X" appears when appropriate

- [ ] **Step 6: Commit**

```bash
git add app/(marketplace)/checkout/page.tsx
git commit -m "migrate checkout to <Price> with native emphasis"
```

---

### Task 22: Buyer order history migration

**Files:**
- Modify: `app/(marketplace)/account/orders/page.tsx`
- Modify: `app/(marketplace)/account/orders/_components/order-list.tsx`
- Modify: `app/(marketplace)/account/orders/[id]/_components/order-items-card.tsx`
- Modify: `app/(marketplace)/account/orders/[id]/_components/order-summary-card.tsx`

**Emphasis:** `native` + `showRateHint`.

- [ ] **Step 1: Apply pattern to each file**

For each `formatPrice(x)` or `formatPrice(x, currency)` call in the 4 files:

```tsx
<Price
  amount={x}
  currency={order.currency /* or item.sku.spu.currency */}
  emphasis="native"
  showRateHint
/>
```

**Where does `order.currency` come from?** Check `core/order/order.buyer.ts` for the order type. The order likely has items each with their own currency (since orders are split per-seller). Use each item's currency for its rows and the order's single currency for totals.

- [ ] **Step 2: Handle "Free" branch in summary card**

Line 41 of order-summary-card.tsx:
```tsx
{transportCost === 0 ? "Free" : (
  <Price amount={transportCost} currency={order.currency} emphasis="native" showRateHint />
)}
```

- [ ] **Step 3: Manual verify**

Navigate to `/account/orders`. Verify:
- Order items show native price
- Totals show native price
- "at current rate" hint visible on converted line
- Free transport still shows "Free"

- [ ] **Step 4: Commit**

```bash
git add app/(marketplace)/account/orders/
git commit -m "migrate buyer order history to <Price> (native + rate hint)"
```

---

### Task 23: Seller dashboard migration (native-only)

**Files:**
- Modify: `app/(marketplace)/seller/page.tsx`
- Modify: `app/(marketplace)/seller/products/page.tsx`
- Modify: `app/(marketplace)/seller/products/[id]/edit/page.tsx`

**Emphasis:** `native-only`.

- [ ] **Step 1: Determine seller currency**

Seller has products in a specific currency. For the dashboard:
- If all seller products share one currency → use it
- If mixed → show "Multi-currency" label, disable conversion

Easiest: fetch seller's first product's currency or expose a seller-level currency. For now, assume all products share currency; use `seller.currency` if the seller type has it, otherwise derive from first product.

- [ ] **Step 2: seller/page.tsx chart formatters**

Line 348, 414 — chart `format`/`formatValue` props:
```diff
-format={formatPrice}
+format={(v: number) => formatMoney(v, sellerCurrency)}
```

Line 454:
```diff
-<p className="font-medium">{formatPrice(product.revenue)}</p>
+<p className="font-medium">
+  <Price amount={product.revenue} currency={product.currency} emphasis="native-only" />
+</p>
```

- [ ] **Step 3: seller/products pages**

Replace each `formatPrice(x)` with `<Price amount={x} currency={product.currency} emphasis="native-only" />`.

- [ ] **Step 4: Manual verify**

Navigate to `/seller` and related pages. Verify:
- Revenue charts use seller's native currency
- No "≈ converted" lines anywhere in seller UI
- Edit product page shows SKU prices in product's native currency

- [ ] **Step 5: Commit**

```bash
git add app/(marketplace)/seller/page.tsx \
        app/(marketplace)/seller/products/page.tsx \
        app/(marketplace)/seller/products/[id]/edit/page.tsx
git commit -m "migrate seller dashboard to native-only prices"
```

---

### Task 24: Seller orders migration (native-only)

**Files:**
- Modify: `app/(marketplace)/seller/orders/page.tsx`
- Modify: `app/(marketplace)/seller/orders/[id]/page.tsx`

**Emphasis:** `native-only` (seller revenue is in their native).

- [ ] **Step 1: Apply pattern to all formatPrice calls**

Each call becomes:
```tsx
<Price amount={x} currency={order.currency /* or item.sku.spu.currency */} emphasis="native-only" />
```

Lines: 269, 270, 279, 317, 323, 338, 472 in `orders/page.tsx`; 249, 353, 358, 363 in `orders/[id]/page.tsx`.

- [ ] **Step 2: Manual verify**

Navigate to `/seller/orders`. Verify no converted lines, all prices in native.

- [ ] **Step 3: Commit**

```bash
git add app/(marketplace)/seller/orders/
git commit -m "migrate seller orders to native-only prices"
```

---

### Task 25: Remove formatPrice from lib/utils.ts

**Files:**
- Modify: `lib/utils.ts`

- [ ] **Step 1: Delete formatPrice + currencyLocaleMap**

In `lib/utils.ts`:

```diff
-const currencyLocaleMap: Record<string, string> = {
-  VND: "vi-VN",
-  USD: "en-US",
-  EUR: "de-DE",
-  JPY: "ja-JP",
-  KRW: "ko-KR",
-}
-
-export function formatPrice(price: number, currency: string = "VND"): string {
-  const locale = currencyLocaleMap[currency] || "vi-VN"
-  return new Intl.NumberFormat(locale, {
-    style: "currency",
-    currency,
-  }).format(price)
-}
```

- [ ] **Step 2: Build — TS compiler surfaces stragglers**

Run: `bunx tsc --noEmit`
Expected: zero errors. Any remaining `formatPrice` import surfaces here — fix them all.

- [ ] **Step 3: Run tests**

Run: `bunx vitest run`
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add lib/utils.ts
git commit -m "remove formatPrice from lib/utils.ts"
```

---

### Task 26: Docs update

**Files:**
- Modify: `CLAUDE.md` (frontend)
- Modify: `shopnexus-server/internal/module/common/README.md`
- Modify: `shopnexus-server/internal/module/account/README.md`

- [ ] **Step 1: FE CLAUDE.md**

In `../shopnexus-website-v3/CLAUDE.md`, update the utilities section:

```diff
-└── utils.ts                # Utility functions (cn, formatPrice)
+└── utils.ts                # Utility functions (cn, formatDate, formatSoldCount, formatTimeAgo)
+└── money.ts                # Money utilities (formatMoney, convertMoney, formatPriceInline)
+core/common/currency.ts     # useExchangeRates, usePreferredCurrency
+components/ui/price.tsx     # <Price /> component for all price displays
```

Remove the `formatPrice(29.99) // "$29.99"` example. Replace with `<Price amount={2999} currency="USD" />`.

- [ ] **Step 2: Common README**

In `internal/module/common/README.md`, add a section:

```markdown
## Exchange Rates

Goroutine-based cron (`SetupExchangeCron`) fetches rates from Frankfurter
every 6h (configurable via `exchange.refresh_interval`). Rates stored in
`common.exchange_rate` keyed on (base, target). Use `ConvertAmount` biz
method for BE-side conversions; FE reads raw snapshot via
`GET /api/v1/common/currencies/rates`.

Config: `exchange.{base,supported,refresh_interval,upstream_url,...}`.
```

- [ ] **Step 3: Account README**

In `internal/module/account/README.md`, add:

```markdown
## Profile Settings

`account.profile.settings` is a JSONB column holding user preferences.
Typed view: `accountmodel.ProfileSettings`. Unknown keys are preserved
across updates (load-merge-write). Update via `PATCH /me/settings`.

Current fields: `preferred_currency` (ISO 4217, validated against
config.Exchange.Supported whitelist).
```

- [ ] **Step 4: Commit**

```bash
# From server repo
git add internal/module/common/README.md internal/module/account/README.md
git commit -m "document profile settings and exchange rate module"

# From website repo
git add CLAUDE.md
git commit -m "update CLAUDE.md for Price component and money utilities"
```

---

## Phase 4 — Final verification

### Task 27: Full QA checklist in browser

- [ ] **Step 1: Start backend + frontend**

```bash
# Terminal 1 (server)
make dev

# Terminal 2 (website)
cd ../shopnexus-website-v3 && bun dev
```

- [ ] **Step 2: Walk through QA checklist**

Login as a user. Go to `/account/settings`, set preferred_currency to `VND`. Then go to the following pages and verify:

- [ ] Browse products with `preferred=VND` → VND products single-line, non-VND products show both
- [ ] Change preferred to USD in settings → page refreshes, VND products now show both lines
- [ ] Product detail: price range, selected SKU, strikethrough original all render correctly
- [ ] Add products from 2 sellers (different currencies if available) → cart page shows grouped subtotals + grand total
- [ ] Checkout: native dominant, converted muted, "You will be charged in X" hint
- [ ] Settings currency picker works, search works, Popular group visible
- [ ] Seller dashboard: native only, no converted lines
- [ ] Seller orders: native only
- [ ] Order history: native dominant, "at current rate" hint visible
- [ ] Wallet (preferred=USD): shows ₫ + ≈ $ two lines
- [ ] Wallet (preferred=VND): single line
- [ ] Throttle network in devtools, reload → Prices gracefully show native-only, no JS errors
- [ ] Direct API test: `curl PATCH /me/settings -d '{"preferred_currency":"XYZ"}'` → 400

- [ ] **Step 3: Run full test suite**

Server: `go test ./... && go vet ./...`
Website: `bunx vitest run && bunx tsc --noEmit`

Expected: all pass.

- [ ] **Step 4: Final commit (if any stragglers)**

```bash
git status
# If anything remaining, commit; otherwise skip
```

---

## Execution Notes

**Parallelization**: Phase 1 (Tasks 1–11) must be sequential due to schema + SQLC dependencies. Phase 2 Tasks 12–16 can run in parallel (no cross-file deps). Phase 3 Tasks 17–24 are fully parallel (each touches distinct files). Task 25 must come last (removes formatPrice). Task 26 docs can happen anywhere after Phase 3.

**Subagent dispatch suggestion:**
- Wave 1: Tasks 1 → 2 → 3 (schema + sqlc + provider, one subagent per task, sequential)
- Wave 2: Tasks 4, 5, 6, 8 in parallel (independent biz units)
- Wave 3: Tasks 7, 9, 10, 11 sequential (wiring depends on biz methods)
- Wave 4: Tasks 12, 13, 14 in parallel (FE foundation)
- Wave 5: Tasks 15, 16 in parallel (FE components)
- Wave 6: Tasks 17, 18, 19, 20, 21, 22, 23, 24 all in parallel (migrations)
- Wave 7: Task 25 (remove formatPrice)
- Wave 8: Task 26 (docs)
- Wave 9: Task 27 (manual QA)

**Commit discipline**: each task ends with its own commit; never batch. Rollback-friendly.
