# TikZ Diagrams for ShopNexus LaTeX Report

## Scope

Replace 11 placeholder figures in the LaTeX thesis with TikZ-drawn diagrams. Skip 5 UI screenshots (user will capture from live app).

## Packages

Add to `thesis.cls`:
- `pgf-umlsd` — sequence diagrams
- TikZ libraries: `shapes.geometric`, `arrows.meta`, `positioning`, `fit`, `backgrounds`, `calc`

## Color Convention

All diagrams use `headerblue` (RGB 41,65,122) as primary accent. Backgrounds use `rowgray` (RGB 240,243,250).

## Diagrams

### 1. fig:order-flow (2-theory.tex)
- **Type**: Activity diagram with swimlanes
- **Lanes**: Buyer | ShopNexus | Seller
- **Flow**: Giỏ hàng → Checkout → Seller xác nhận → Thanh toán → Giao vận → [fork] Hoàn trả → Dispute
- **Source**: `internal/module/order/biz/interface.go` method names

### 2. fig:tech-stack (2-theory.tex)
- **Type**: Stacked layer-cake diagram
- **Layers** (top→bottom): Frontend (Next.js 16, React 19, shadcn/ui) → API (ConnectRPC, Echo v4) → Backend (Go, Restate, Uber fx) → Data (PostgreSQL, Redis, Milvus, MinIO) → Infra (Docker Compose, NATS)
- **Source**: `deployment/docker-compose.yml`, `go.mod`

### 3. fig:workflow-truyen-thong (3-analysis/current-state.tex)
- **Type**: Activity diagram with swimlanes
- **Lanes**: Khách hàng | Người bán
- **Flow**: Đặt hàng → Xác nhận thủ công → Đóng gói → Giao hàng → Hoàn trả thủ công
- **Annotations**: Dashed red notes highlighting pain points ("thủ công", "không truy vết", "dễ sai sót")

### 4. fig:workflow-shopnexus (3-analysis/current-state.tex)
- **Type**: Collaboration diagram
- **Nodes**: Khách hàng, Người bán, ShopNexus (center), Cổng thanh toán, Đơn vị vận chuyển
- **Messages**: Numbered 1–5 with descriptions

### 5. fig:overall-usecase (3-analysis/usecases-common.tex)
- **Type**: Use case diagram
- **Actors**: Buyer (left), Seller (right), Admin (bottom-right)
- **9 groups**: Giỏ hàng, Đặt hàng, Xử lý đơn, Thanh toán, Theo dõi, Hoàn trả, Tranh chấp, Dashboard, Đánh giá
- **Relations**: `<<include>>`, `<<extend>>`, generalization
- **Source**: `usecases-common.tex` UC01–UC30 list

### 6. fig:order-sequence (3-analysis/order.tex)
- **Type**: Sequence diagram (pgf-umlsd), analysis level
- **Lifelines**: Buyer, ShopNexus, Catalog, Inventory, PaymentGW, Seller
- **Key messages with params**: `checkout(items[], buyNow)`, `getSKU(skuID)`, `reserve(skuID, qty)`, `confirm(itemIDs, transportOpt)`, `pay(orderIDs, paymentOpt)`, `confirmPayment(refID, status)`
- **Source**: `biz/checkout.go`, `biz/incoming.go`, `biz/pay.go`

### 7. fig:refund-sequence (3-analysis/refund.tex)
- **Type**: Sequence diagram (pgf-umlsd), analysis level
- **Lifelines**: Buyer, ShopNexus, Seller, Admin
- **Key messages**: `createRefund(orderID, method, reason)`, `confirmRefund(refundID)`, `createDispute(refundID, reason)`, `resolveDispute(disputeID, status)`
- **Source**: `biz/refund.go`

### 8. fig:architecture (4-design/architecture.tex)
- **Type**: Layered architecture diagram
- **3 layers**: Transport (Echo) → Biz (Restate Handlers) → Data (SQLC/pgx)
- **8 modules** in Biz layer: account, catalog, order, inventory, promotion, analytic, chat, common
- **Restate Ingress** box connecting modules
- **Infra row**: PostgreSQL, Redis, Milvus, MinIO, NATS
- **External**: VNPay, GHTK, Nominatim, OpenAI
- **Source**: `internal/module/*/fx.go`, `deployment/docker-compose.yml`

### 9. fig:order-sequence (4-design/order.tex)
- **Type**: Sequence diagram (pgf-umlsd), design level (internal components)
- **Lifelines**: BuyerForm, OrderHandler, CatalogProxy, InventoryProxy, PaymentProvider, DB
- **Messages map to API names**: `BuyerCheckout()`, `ConfirmSellerPending()`, `PayBuyerOrders()`
- **Source**: `biz/interface.go`, `biz/checkout.go`, `biz/incoming.go`

### 10. fig:refund-sequence (4-design/refund.tex)
- **Type**: Sequence diagram (pgf-umlsd), design level
- **Lifelines**: RefundForm, OrderHandler, PaymentProvider, TransportProvider, DB
- **Messages**: `CreateBuyerRefund()`, `ConfirmSellerRefund()`, auto-refund via payment provider
- **Source**: `biz/refund.go`

### 11. fig:order-erd (4-design/database.tex)
- **Type**: ERD with crow's foot notation
- **7 tables**: cart_item, item, order, payment, transport, refund, refund_dispute
- **4 enums**: item_status, status, transport_status, refund_method
- **Key relationships**: item→order (CASCADE), refund→order (NO ACTION), order→payment (SET NULL), order→transport (SET NULL)
- **Annotation**: Partial index on item WHERE order_id IS NULL
- **Source**: `internal/module/order/db/migrations/0001_init.up.sql`

## Implementation Order

1. Add packages to `thesis.cls`
2. Draw simpler diagrams first (tech-stack, collaboration, activity)
3. Draw use case diagram
4. Draw sequence diagrams (4x, using pgf-umlsd)
5. Draw architecture diagram
6. Draw ERD
