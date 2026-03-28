# ShopNexus Server

[![wakatime](https://wakatime.com/badge/user/592c97c4-15ad-49cb-ac34-d607be35c524/project/79f8a24e-0fe8-417e-b42b-2009d7a4362f.svg)](https://wakatime.com/badge/user/592c97c4-15ad-49cb-ac34-d607be35c524/project/79f8a24e-0fe8-417e-b42b-2009d7a4362f)

A social marketplace backend in Go, built as a **modular monolith** designed for microservice extraction via [Restate](https://restate.dev) durable execution — comprising these modules with clear boundaries:

- [`account`](internal/module/account/) — Auth, profiles, contacts, favorites, payment methods, notifications
- [`catalog`](internal/module/catalog/) — Products (SPU/SKU), categories, brands, tags, comments, hybrid search, recommendations
- [`order`](internal/module/order/) — Cart, checkout, pending items, seller confirmation, payment, refunds
- [`inventory`](internal/module/inventory/) — Stock management, serial tracking, audit history
- [`promotion`](internal/module/promotion/) — Discounts, ship discounts, scheduling, group-based price stacking
- [`analytic`](internal/module/analytic/) — Interaction tracking, weighted product popularity scoring
- [`chat`](internal/module/chat/) — Real-time WebSocket messaging, conversations, read receipts
- [`common`](internal/module/common/) — Resource/file management, object storage, service options, geocoding
- [`system`](internal/module/system/) — Transactional outbox for reliable event publishing

> Development timeline: [timeline.md](timeline.md)

---

## Durable Execution (Restate)

All biz handler methods use `restate.Context` instead of `context.Context`. This gives:

- **Durable side effects**: DB writes inside `restate.Run()` are journaled and replay-safe
- **Cross-module RPC**: calls go through generated Restate proxy clients (auto-registered at startup)
- **Fire-and-forget**: `restate.ServiceSend()` for notifications and analytics tracking — durable, exactly-once delivery
- **Terminal errors**: client-facing errors use `.Terminal()` to prevent Restate retries

Cross-module dependencies use the `XxxBiz` interface (resolved to Restate proxy by fx), not direct struct references. Transport handlers also depend on the interface, not the concrete handler.

---

## Unified Account Model

There are no separate customer/vendor account types. Any account can both buy and sell. Orders track `buyer_id` and `seller_id` per transaction, not per account.

---

## Order Lifecycle

The order flow is split into three phases with clear responsibility boundaries:

### 1. Checkout (Buyer)

Buyer selects SKUs (from cart or Buy Now), provides a shipping address per item. The system:

- Validates SKU availability and fetches seller info
- **Reserves inventory** immediately (prevents overselling)
- Creates **pending `order.item` records** (no order yet, no payment)
- Removes items from cart

Pending items can be cancelled by the buyer (releases inventory).

### 2. Confirmation (Seller)

Seller sees incoming pending items grouped by buyer + address. Seller selects items and confirms them:

- Creates a **transport** record (shipping via pluggable provider)
- Groups selected items into a single **order** with calculated costs:
  - `product_cost` = sum(unit_price × quantity)
  - `product_discount` = product_cost - sum(paid_amount)
  - `total` = product_cost - product_discount + transport_cost
- Items that share the same buyer AND address can be grouped together
- Seller can also reject items (releases inventory, notifies buyer)

### 3. Payment (Buyer)

Buyer sees confirmed orders with exact totals (product + transport). Buyer selects orders and pays:

- Creates a **payment** record linked to the selected orders
- Calls the payment provider (VNPay QR/Bank/ATM or COD)
- Payment verified via IPN callback

### Cancellation

- **Unpaid orders**: buyer can cancel directly (releases inventory)
- **Paid orders**: must go through the refund flow (PickUp or DropOff return methods)

---

## Product Model (SPU/SKU)

Products follow the industry-standard two-level hierarchy:

- **SPU (Standard Product Unit)**: the abstract product concept — name, description, category, brand, specifications, tags, resources (images). Owned by the seller's account.
- **SKU (Stock Keeping Unit)**: a concrete purchasable variant — price, attributes (color/size), package details, inventory stock. Each SKU belongs to one SPU.

A **featured SKU** per SPU determines the display price on product cards.

---

## Hybrid Search & Recommendations

Product search combines **dense vector similarity** (embedding-based) and **sparse BM25** (keyword-based) scoring via Milvus. If Milvus is unavailable, falls back to PostgreSQL `ILIKE` matching.

Background cron jobs sync product metadata and embeddings to Milvus:

- **Metadata sync**: runs frequently, re-indexes name/price/tags/category
- **Embedding sync**: runs less often, regenerates vector embeddings when description changes (expensive)

Personalized recommendations use a Redis-cached feed per user, refreshed from Milvus collaborative filtering. Falls back to most-sold products when recommendations are insufficient.

---

## Promotion Engine

Promotions use **group-based stacking**:

- Promotions in **different groups** stack with each other
- Promotions in the **same group** compete — the one with the biggest savings wins
- An **"exclusive" group** promotion overrides all others

Discount data is stored as JSONB in `promotion.promotion.data` with configurable `min_spend`, `max_discount`, `discount_percent` or `discount_price`.

Promotions can target specific products (SPU/SKU), categories, or brands via the `promotion.ref` table. Auto-apply promotions are included in every price calculation; code-based promotions require the buyer to enter a code.

Types defined: Discount, ShipDiscount, Bundle, BuyXGetY, Cashback. Currently implemented in price calculation: **Discount** and **ShipDiscount**.

---

## Inventory & Serial Tracking

Stock uses a polymorphic `(ref_type, ref_id)` design supporting both ProductSKU and Promotion references.

Serial assignment during checkout uses `FOR UPDATE SKIP LOCKED` — concurrent buyers get different serials without blocking each other. If a transaction rolls back, those serials become available again.

Every stock change (import, reserve, release) is recorded in an append-only `stock_history` audit trail.

---

## Payment & Transport Providers

Both systems use a **pluggable provider pattern** — a `map[string]Client` keyed by option ID, registered at startup.

**Payment providers:**

- VNPay (QR, Bank, ATM) — redirect-based with IPN webhook verification
- COD (Cash on Delivery) — no online payment

**Transport providers:**

- GHTK (Express, Standard, Economy) — mock implementation with weight-based cost calculation

Providers are discoverable at runtime via the common service option registry.

---

## Analytics

Fire-and-forget interaction tracking via Restate durable calls. Weighted popularity scoring with atomic upsert accumulation. Feeds into product search recommendations.

---

## Real-time Chat

WebSocket-based messaging between any two accounts. One conversation per account pair (idempotent creation). Supports text, image, and system messages with read receipt tracking. Messages are persisted to PostgreSQL; offline users retrieve history via REST pagination.
