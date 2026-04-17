# Full Restate Context Conversion

## Goal
Convert all 6 business modules (account, catalog, inventory, promotion, analytic, chat) to use `restate.Context`, remove pubsub, and replace event flows with Restate service calls.

## Modules to convert
account, catalog, inventory, promotion, analytic, chat

## Pubsub removal

### Current event flows
1. `CreateInteraction` → publish("analytic.interaction") → `analytic.HandlePopularityEvent` + `catalog.AddInteraction`
2. `Checkout` → publish("order.created") → `OrderCreated` (empty)
3. `VerifyPayment` → publish("order.paid") → `OrderPaid` (update payment status)
4. `ConfirmOrder` → publish("order.confirmed") → no subscriber

### Replacement
| Current | New |
|---|---|
| `CreateInteraction` → publish → `HandlePopularityEvent` | `CreateInteraction` calls `ctx.Service("AnalyticBiz", "HandlePopularityEvent").Send()` |
| `CreateInteraction` → publish → `catalog.AddInteraction` | `CreateInteraction` calls `ctx.Service("CatalogBiz", "AddInteraction").Send()` |
| `Checkout` → publish → `OrderCreated` (empty) | Remove |
| `VerifyPayment` → publish → `OrderPaid` | Inline payment update in `VerifyPayment` |
| `TrackInteraction` (goroutine) | Callers use `ctx.Service("AnalyticBiz", "CreateInteraction").Send()` |

### Files deleted
- `order/biz/order_pubsub.go`
- `catalog/biz/search_pubsub.go`
- `analytic/biz/popularity.go` → `InitPopularityPubsub` only (keep `HandlePopularityEvent`)

## What stays context.Context
- `SetupMilvusCollections` — one-time init
- `SetupCron` / `StartProductSyncCron` / `SyncProductData` / `UpdateStaleProducts` — cron infra
- Common module — shared utility, not a Restate service
- Order `SetupPaymentMap` / `SetupShipmentMap` — init

## Chat websocket
Websocket handler changes from `*ChatBiz` to `ChatClient` (proxy interface).

## Execution
1. Convert all biz methods to `restate.Context` via sed (except cron/init)
2. Remove pubsub, replace with Restate service calls
3. Delete pubsub files, inline OrderPaid
4. Remove TrackInteraction/TrackInteractions, update callers
5. Update constructors (remove pubsub field where unused)
6. Register all modules in `app/restate.go`
7. Update chat websocket to use ChatClient
8. Regenerate proxies, build, commit
