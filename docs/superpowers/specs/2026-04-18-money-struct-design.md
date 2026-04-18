# Money Struct: Shared Currency+Amount Type

**Date:** 2026-04-18
**Status:** Approved

## Summary

Create a `Money` struct in `internal/shared/model/` that pairs `Amount int64` (minor units) with `Currency string` (ISO 4217). Use it in every domain model field that represents money.

## Struct

```go
// internal/shared/model/money.go
type Money struct {
    Amount   int64  `json:"amount"`
    Currency string `json:"currency"`
}
```

No methods, no custom JSON marshal. Plain struct. Arithmetic done on `.Amount` directly.

## Fields to Change

### Catalog Module

| Struct | Field | Before | After |
|---|---|---|---|
| `ProductSku` | `Price` | `int64` | `Money` |
| `ProductCard` | `Price` | `int64` | `Money` |
| `ProductCard` | `OriginalPrice` | `int64` | `Money` |
| `ProductDetailSku` | `Price` | `int64` | `Money` |
| `ProductDetailSku` | `OriginalPrice` | `int64` | `Money` |
| `OrderPrice` | `ProductCost` | `int64` | `Money` |
| `OrderPrice` | `ShipCost` | `int64` | `Money` |
| `RequestOrderPrice` | `UnitPrice` | `int64` | `Money` |
| `RequestOrderPrice` | `ShipCost` | `int64` | `Money` |

### Order Module

| Struct | Field | Before | After |
|---|---|---|---|
| `Order` | `ProductCost` | `int64` | `Money` |
| `Order` | `ProductDiscount` | `int64` | `Money` |
| `Order` | `TransportCost` | `int64` | `Money` |
| `Order` | `Total` | `int64` | `Money` |
| `OrderItem` | `UnitPrice` | `int64` | `Money` |
| `Payment` | `Amount` + `buyer/seller_currency` | `int64` + 2 strings | `BuyerAmount Money` + `SellerAmount Money` |
| `Payment` | `ExchangeRate` | `float64` | `float64` (unchanged) |
| `Transport` | `Cost` | `int64` | `Money` |

### Account Module

| Struct | Field | Before | After |
|---|---|---|---|
| `Wallet.Balance` | `Balance` | `int64` | `Money` |

### Providers

| Location | Field | Before | After |
|---|---|---|---|
| Payment provider params | `Amount` | `int64` | `Money` |
| GHTK provider | `Cost` | `int64` | `Money` |

### Unchanged

| Location | Reason |
|---|---|
| Promotion `DiscountData` (min_spend, max_discount, discount_price) | JSONB fields — currency lives at promotion table level, not per-field |

## Arithmetic Pattern

```go
total := sharedmodel.Money{
    Amount:   op.ProductCost.Amount + op.ShipCost.Amount,
    Currency: op.ProductCost.Currency,
}
```

## DB Mapping

No DB schema changes. Currency columns already exist from previous migration. Biz layer populates `Money.Currency` when mapping DB row to domain model:

```go
// Example: ProductSku
Price: sharedmodel.Money{Amount: sku.Price, Currency: sku.Currency}
```

## Files Affected

- Create: `internal/shared/model/money.go`
- Modify: `internal/module/catalog/model/catalog.go`
- Modify: `internal/module/catalog/model/product.go`
- Modify: `internal/module/catalog/model/product_card.go`
- Modify: `internal/module/catalog/model/product_detail.go`
- Modify: `internal/module/order/model/order.go`
- Modify: `internal/module/catalog/biz/product_sku.go`
- Modify: `internal/module/catalog/biz/search_sync.go`
- Modify: `internal/module/promotion/biz/price.go`
- Modify: `internal/module/order/biz/checkout.go`
- Modify: `internal/module/order/biz/order.go`
- Modify: `internal/module/order/biz/refund.go`
- Modify: `internal/provider/transport/ghtk/ghtk.go`
- Modify: `internal/provider/payment/payment.go`
