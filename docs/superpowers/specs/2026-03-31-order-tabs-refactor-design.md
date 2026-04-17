# Order Pages Tab Refactor

## Goal

Simplify buyer and seller order pages from many tabs to focused views. Merge pending-items and incoming pages inline as tabs. Remove standalone pages.

## Buyer: `/account/orders`

### Tabs

| Tab | Content | Badge states |
|-----|---------|-------------|
| **Pending** | Order items awaiting seller approval + orders that are confirmed-but-unpaid + paid-confirmed-not-delivered (Confirmed, Shipped) | "Awaiting Approval", "Unpaid", "Confirmed", "Shipped" |
| **Completed** | Orders with status = Delivered | "Delivered" |
| **Cancelled** | Orders with status = Cancelled | "Cancelled" |

### Pending tab card format

Two card types sharing the same visual shell (image, product name, qty, price, status badge):

1. **Item card** (from `useListBuyerPending`): individual order item not yet part of an order. Badge: "Awaiting Approval". Shows cancel button if status is Pending.
2. **Order card** (from `useListBuyerConfirmed` filtered to Pending/Confirmed/Shipped status): order with items listed inside. Badge reflects order status ("Unpaid" if payment=null, otherwise "Confirmed" or "Shipped").

### Data sources

- `useListBuyerPending({ limit: 20 })` for item-level pending items
- `useListBuyerConfirmed({ limit: 20, status: ["Pending", "Confirmed", "Shipped"] })` for active orders

Both lists paginate independently. The Pending tab renders pending items first, then active orders below.

### Removals

- Delete tabs: "All", "Unpaid", "Active"
- Delete page: `/account/pending-items/` (page.tsx + _components/item-list.tsx)
- Remove "Pending Items" preview section from orders page

## Seller: `/seller/orders`

### Tabs

| Tab | Content |
|-----|---------|
| **Incoming** | Pending items from buyers — full `/seller/incoming` functionality moved inline |
| **Confirmed** | All non-pending orders (Confirmed + Shipped + Delivered) |

### Incoming tab

Identical to current `/seller/incoming` page:
- Search by SKU name, ID, or buyer
- Items grouped by buyer + address
- Checkbox bulk select with Select All
- Confirm dialog: transport option picker (radio group) + optional note
- Reject dialog: confirmation before rejecting
- Load more pagination
- Uses `useListSellerPending()`, `useConfirmSellerPending()`, `useRejectSellerPending()`

### Confirmed tab

- Shows orders from `useListSellerConfirmed()` (no status filter — includes Confirmed, Shipped, Delivered)
- Search by order ID
- Each card: order summary, status badge, buyer name, item count, total, dropdown with "View Details"
- Load more pagination

### Removals

- Delete tabs: "All orders", "Shipped", "Delivered"
- Delete page: `/seller/incoming/` (page.tsx)
- Remove "Incoming Items" preview section from orders page

## Sidebar

No changes. Neither pending-items nor incoming were sidebar entries.

## Files affected

### Frontend (shopnexus-website-v3)

| Action | Path |
|--------|------|
| Rewrite | `app/(marketplace)/account/orders/page.tsx` |
| Rewrite | `app/(marketplace)/seller/orders/page.tsx` |
| Delete | `app/(marketplace)/account/pending-items/page.tsx` |
| Delete | `app/(marketplace)/account/pending-items/_components/item-list.tsx` |
| Delete | `app/(marketplace)/seller/incoming/page.tsx` |

### Backend

No backend changes required. Existing endpoints cover all data needs.
