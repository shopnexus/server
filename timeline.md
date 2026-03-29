# Development Timeline

## 29-8-2025 First request only take 10ms

![img.png](images/img.png)

#### N+1 query btw but still blazingly fast

![img.png](images/img2.png)

## 4-9-2025 Found a way to write better queries with sqlc.slice

I should create a PR to sqlc.dev documentation haha

```sql
SELECT *
FROM "catalog"."product_spu"
WHERE (
    ("id" = ANY (sqlc.slice('id')))
)
```

## 5-9-2025 List products with calculated sale price (from many nested queries into 6 flat queries) only take 20ms for 10 products

![img.png](images/img3.png)

## 8-9-2025 Custom type need to be registered to pgx (pgxpool.go)

Any custom DB types made with CREATE TYPE need to be registered with pgx.
<https://github.com/kyleconroy/sqlc/issues/2116>
![img.png](images/img4.png)

## 13-9-2025 Nice integration of enum fields between validator/v10 validation and sqlc-generated Valid() methods

With "emit_enum_valid_method: true" in sqlc.yaml and "validateFn=Valid" in struct tag
I can validate the enum field directly with the generated Valid() method from sqlc.

```go
type CreateOrderParams struct {
 Account     accountmodel.AuthenticatedAccount
 Address     string                `validate:"required"`
 OrderMethod db.OrderPaymentMethod `validate:"required,validateFn=Valid"`
 SkuIDs      []int64               `validate:"required,dive,gt=0"`
}
```

I should write a blog on this btw.

## 15-9-2025 Implement a well-structured custom Pub/Sub client for clean, maintainable publish/subscribe code

```go
// The subcriber
func (s *orderBiz) SetupPubsub() error {
    return errutil.Some(
        s.pubsub.Subscribe("order.created", pubsub.DecodeWrap(s.OrderCreated)),
        s.pubsub.Subscribe("order.paid", pubsub.DecodeWrap(s.OrderPaid)),
    )
}

type OrderCreatedParams = struct {
    OrderID int64
}

func (s *orderBiz) OrderCreated(ctx context.Context, params OrderCreatedParams) error {
    // code here

    return nil
}

type OrderPaidParams = struct {
    OrderID int64
    Amount  int64
}

func (s *orderBiz) OrderPaid(ctx context.Context, params OrderPaidParams) error {
    // code here

    return nil
}

// The publisher
if err = s.pubsub.Publish("order.created", OrderCreatedParams{
    OrderID: order.ID,
}); err != nil {
    return zero, err
}
```

With this approach, I can easily add new event handlers by simply defining a new struct for the event parameters and implementing the corresponding handler method.
Also when finding subcribers, I can search globally by "OrderCreated*" or "OrderPaid*" to find all related handlers because the handler name is the same as the event name.

## 25-9-2025 First demo of recommendation engine with milvus vector search

**No more elasticsearch:**

- Elasticsearch is great, but vector databases are the future.
- After certain days with elasticsearch, found it is not suitable for vector search.
- As I remember, I was using model MGTE (alibaba) storing 200rows took 8mb of storage
![img.png](images/img5.png)
- Inserting into milvus took 60seconds per 100 products
![img.png](images/img6.png)

## 7-10-2025 Refactor payment and shipment with better interface

- Maintainer will now easier to add new payment gateway or shipment provider

```go
func (s *orderBiz) SetupPaymentMap() error {
 var configs []sharedmodel.OptionConfig

 s.paymentMap = make(map[string]payment.Client) // map[gatewayID]payment.Client

 // setup cod client
 codClient := cod.NewClient()
 s.paymentMap[codClient.Config().ID] = codClient
 configs = append(configs, codClient.Config())

 // setup vnpay client
 vnpayClients := vnpay.NewClients(vnpay.ClientOptions{
  TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
  HashSecret: config.GetConfig().App.Vnpay.HashSecret,
  ReturnURL:  config.GetConfig().App.Vnpay.ReturnURL,
 })
 for _, c := range vnpayClients {
  s.paymentMap[c.Config().ID] = c
  configs = append(configs, c.Config())
 }

 if err := s.shared.UpdateServiceOptions(context.Background(), "payment", configs); err != nil {
  return err
 }

 return nil
}
```

- Create shared service option table to store the payment and shipment options

```sql
CREATE TABLE "shared"."service_option" (
    "id" VARCHAR(100) NOT NULL,
    "category" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    "provider" TEXT NOT NULL,
    "method" TEXT NOT NULL,
    "is_active" BOOLEAN NOT NULL DEFAULT true,

    CONSTRAINT "service_option_pkey" PRIMARY KEY ("id")
);
```

## 30-10-2025 After a long time of lazying around

## 7-11-2025 Refactor database wrapper (storage)

Add transaction callback to storage interface to reduce boilerplate code when using transaction. Back then I always forget to commit/rollback the transaction

```go
// WithTx executes the given function within a transaction, prefer using the provided Storage if not nil, automatically commit/rollback
 WithTx(ctx context.Context, preferStorage Storage, fn func(txStorage Storage) error) error
```

- With this approach, you can pass the preferStorage from outer biz layer to inner biz layer when both layers need to use transaction. Eg: CreateComment which calls UpdateResources atomically.
- You can choose to have a nested transaction by setting allowNestedTx (default: false) to true in NewTxQueries.

![img.png](images/img7.png)

## 8-11-2025 Use errors.Join instead of my own errutil.Some

```go
func Some(errs ...error) error {
 for _, err := range errs {
  if err != nil {
   return err
  }
 }
 return nil
}

// Standard library approach (Go 1.20+)
err := errors.Join(err1, err2, err3)
// Returns an error containing all non-nil errors

// Your Some function
err := errutil.Some(err1, err2, err3)
// Returns only the first non-nil error
```

## 18-11-2025 Making big update for entire project

- Each service has its own storage interface (db) to reduce coupling between services
- Refactor pgsqlc module to support generic
- Refactor entire order schema to support both multi-vendor and single-vendor ecommerce systems
- Now support register all custom types for encode plans in pgxpool instead of hardcoded type names (internal/infras/pg/pg.go)
- Remove the global config.GetConfig() calls, pass the config struct to each service biz layer instead for better testability and reduce coupling

![alt text](images/img8.png)

## 1-12-2025 Database per service

- Refactor all modules to have their own PostgreSQL schema (catalog.*, order.*, account.*, etc.)
- Move shared module outside of modules folder to avoid confusion between common & shared
- Add migrations per module instead of single global migration
- Add DBML schema documentation

## 10-12-2025 Migration CLI and cursor pagination

- Add migration CLI tool for running migrations programmatically
- Add cursor-based encoder/decoder for pagination (alongside offset pagination)
- Temporal transaction storage experiment

## 15-12-2025 Upgrade docker compose

- Update docker-compose for multi-node Restate cluster (3 nodes)

## 23-3-2026 Start using Claude Code - please dont think I'm braindead, I just want to have a more organized codebase and avoid the mess of random files and patterns that I might forget after a long time

- Vibe some tools to generate boilerplate code or erdiagrams markdown
- Add pgtempl `-module all` flag to generate queries for all modules at once

## 25-3-2026 Decouple cross-module dependencies with Restate

The big architecture shift: instead of direct function calls between modules, every cross-module call now goes through Restate HTTP ingress. This enables future microservice extraction.

- Decouple cross-module deps: use `XxxBiz` interfaces instead of `*XxxHandler` pointers
- Auto-generate Restate HTTP proxy clients from interface definitions (`cmd/genrestate`)
- Rename: `XxxClient` → `XxxBiz` (interface), `XxxBiz` → `XxxHandler` (struct)

<!-- image: diagram showing module A calling module B through Restate ingress -->

## 27-3-2026 Massive schema and API refactor

- Merge customer/vendor into single account (any account can both buy and sell)
- Decouple checkout from order creation (checkout → pending items → seller confirms → order created)
- Add notifications system (in-app notifications via Restate fire-and-forget)
- Add geocoding with Nominatim (OpenStreetMap) for address search
- Add LLM package with Client interface supporting Python/OpenAI/Bedrock providers
- Wrap all bare error returns with context across all biz handlers

<!-- image: screenshot of the new checkout flow (pending items → confirm → order) -->

## 28-3-2026 Rewrite all module READMEs with auto-generated Mermaid diagrams

- Add script to generate Mermaid ER diagrams from migration SQL files
- Each module README now has an auto-generated schema diagram
- Restructure infras → provider (geocoding, payment, transport)

## 30-3-2026 The big feature sprint

### Remove brand concept

- Brand was over-engineering, replaced with a "Brand" specification entry on products
- Delete brand table, queries, biz, transport, and all FE references

### Fix Restate error propagation

One of the trickiest bugs: HTTP status codes were being lost when errors crossed Restate service boundaries. A 409 Conflict from inventory became a 500 Internal Server Error at the API layer.

- Parse error messages from Restate JSON responses to extract original error text
- Preserve HTTP status codes in terminal errors across service calls
- Fix `WrapErr` to not duplicate `[CODE]` prefix when wrapping terminal errors

### Enrich product detail page

- Add total sold count (from inventory stock taken)
- Add vendor stats (product count, average rating, total sold, response rate)
- Add stock status, price range, share button (Web Share API), tags display
- Wire wishlist (add/remove favorite)
- Buy Now confirmation dialog with price breakdown

<!-- image: screenshot of the enriched product detail page -->

### Seller product management

- Product edit page with dirty tracking (`useDirty` hook) for PATCH-only updates
- Shared `ProductSPUForm` component for create/edit with `formKey` for Quill remount
- Stock management dialog with serial support (auto-generate vs custom IDs)
- Stale embedding/metadata badges on product listing

<!-- image: screenshot of the seller product edit page with stock dialog -->

### Fix resource reference deletion bug

Critical bug: `DeleteResourceReference` was called without `RefID` filter, causing `nil` RefID to match ALL rows via `$4 IS NULL` SQL clause. After creating a product with 4 images, it would delete ALL resource references in the system.

### Optimize search sync

- Search sync cron now rechecks `is_stale_embedding`/`is_stale_metadata` before processing
- Stale flags only cleared after successful embedding + Milvus upsert
- Strip HTML from descriptions before embedding using `x/net/html` tokenizer

### Seed data overhaul

- Refactor seed into 6 files with proper transaction support (per product)
- 5 vendor accounts with bcrypt passwords and faker profiles
- 3-12 enriched reviews per product with multiple reviewers

### Redesign payment client interface

Major payment system redesign with provider self-registration pattern:

```go
type Client interface {
    Config() sharedmodel.OptionConfig
    Create(ctx, CreateParams) (CreateResult, error)
    Get(ctx, providerID) (PaymentInfo, error)
    OnResult(fn ResultHandler)
    InitializeWebhook(e *echo.Echo)
    Charge(ctx, ChargeParams) (ChargeResult, error)
    Refund(ctx, RefundParams) (RefundResult, error)
    Tokenize(ctx, TokenizeParams) (TokenizeResult, error)
}
```

- Each provider registers its own webhook route via `OnResult` + `InitializeWebhook`
- Add SePay payment gateway (Vietnamese hosted checkout with HMAC-SHA256 signing)
- Add stub card payment provider for future credit/debit card support
- `Charge`/`Refund`/`Tokenize` for saved card payments (interface ready, processor TBD)
- Remove COD payment

### Payment method refactor

- Buyers can save credit/debit cards (tokenized) for one-click payments
- `PayOrders` branches on `pm:` prefix (card charge) vs redirect option slug
- Auto-refund to original card when refund is confirmed
- Payment method selector dialog on buyer order pages (saved cards + redirect providers)
- Redirect URLs now open in new tab with "Complete Payment" re-open button
- Store redirect URL in payment `data` JSONB for re-opening

<!-- image: screenshot of the payment method selector dialog -->

### Wire product images everywhere

- All 6 order/item pages (seller orders, seller order detail, incoming items, buyer orders, buyer order detail, pending items) now show product images instead of Package icon placeholder

### Search and filter APIs

- Add server-side search to incoming items (ILIKE on sku_name)
- Add server-side search + status filter to seller orders
- FE debounced search passed to API instead of client-side filtering

### Milvus timeout

- Add `WithTimeout(d)` fluent API to Milvus client
- All Milvus operations (Query, Search, HybridSearch) respect timeout
- Prevents 22s hangs when Milvus is down (now fails in 5s)
