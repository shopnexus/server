# Order Pages Tab Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Simplify buyer and seller order pages to 3 and 2 tabs respectively, merging pending-items and incoming pages inline.

**Architecture:** Rewrite the two order page.tsx files. Buyer gets Pending/Completed/Cancelled tabs where Pending combines item-level pending items with active orders. Seller gets Incoming/Confirmed tabs where Incoming embeds the full bulk-action workflow from `/seller/incoming`. Delete standalone pages.

**Tech Stack:** Next.js 16, React 19, TanStack Query, shadcn/ui, Tailwind CSS

---

## File Structure

| Action | Path | Responsibility |
|--------|------|----------------|
| Rewrite | `app/(marketplace)/account/orders/page.tsx` | Buyer orders: 3 tabs (Pending, Completed, Cancelled) |
| Keep | `app/(marketplace)/account/orders/_components/order-list.tsx` | Reusable order card list (already exists, used by Completed/Cancelled) |
| Rewrite | `app/(marketplace)/seller/orders/page.tsx` | Seller orders: 2 tabs (Incoming, Confirmed) |
| Delete | `app/(marketplace)/account/pending-items/page.tsx` | Replaced by Pending tab |
| Delete | `app/(marketplace)/account/pending-items/_components/item-list.tsx` | Replaced by Pending tab |
| Delete | `app/(marketplace)/seller/incoming/page.tsx` | Replaced by Incoming tab |

---

### Task 1: Rewrite buyer orders page

**Files:**
- Modify: `../shopnexus-website-v3/app/(marketplace)/account/orders/page.tsx`

- [ ] **Step 1: Replace the entire file with the new 3-tab implementation**

```tsx
"use client"

import { useMemo } from "react"
import Image from "next/image"
import Link from "next/link"
import {
  useListBuyerPending,
  useListBuyerConfirmed,
  useCancelBuyerPending,
  TOrderItem,
  TOrder,
} from "@/core/order/order.buyer"
import { ProductLink } from "@/components/product/product-link"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { OrderList } from "./_components/order-list"
import {
  Package,
  Clock,
  CheckCircle,
  XCircle,
  Truck,
  CreditCard,
  Loader2,
  ShoppingBag,
  Inbox,
} from "lucide-react"
import { formatPrice, cn } from "@/lib/utils"
import { toast } from "sonner"
import { useState } from "react"

// ===== Pending Items Section =====

function PendingItemCard({ item, onCancel }: { item: TOrderItem; onCancel: (id: number) => void }) {
  const badgeColor = "bg-yellow-100 text-yellow-800"

  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-center gap-3">
          <div className="relative h-16 w-16 rounded bg-muted flex items-center justify-center flex-shrink-0">
            {item.resources?.[0] ? (
              <Image src={item.resources[0].url} alt={item.sku_name} fill className="object-cover rounded" />
            ) : (
              <Package className="h-6 w-6 text-muted-foreground" />
            )}
          </div>
          <div className="flex-1 min-w-0">
            <ProductLink spuId={item.spu_id}>{item.sku_name}</ProductLink>
            <p className="text-sm text-muted-foreground">
              Qty: {item.quantity} x {formatPrice(item.unit_price)}
            </p>
            {item.note && (
              <p className="text-sm text-muted-foreground truncate">{item.note}</p>
            )}
          </div>
          <div className="flex flex-col items-end gap-2 flex-shrink-0">
            <Badge variant="secondary" className={cn("font-normal gap-1", badgeColor)}>
              <Clock className="h-3 w-3" />
              Awaiting Approval
            </Badge>
            <p className="text-sm font-medium">{formatPrice(item.unit_price * item.quantity)}</p>
            {item.status === "Pending" && (
              <Button variant="ghost" size="sm" className="text-destructive h-7 px-2" onClick={() => onCancel(item.id)}>
                Cancel
              </Button>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function PendingTab() {
  const {
    data: pendingData,
    isLoading: pendingLoading,
    fetchNextPage: fetchMorePending,
    hasNextPage: hasMorePending,
    isFetchingNextPage: fetchingMorePending,
  } = useListBuyerPending({ limit: 20 })

  const {
    data: ordersData,
    isLoading: ordersLoading,
    fetchNextPage: fetchMoreOrders,
    hasNextPage: hasMoreOrders,
    isFetchingNextPage: fetchingMoreOrders,
  } = useListBuyerConfirmed({ limit: 20, status: ["Pending", "Confirmed", "Shipped"] } as any)

  const cancelMutation = useCancelBuyerPending()
  const [cancelId, setCancelId] = useState<number | null>(null)

  const pendingItems = useMemo(
    () => pendingData?.pages.flatMap((p) => p.data) ?? [],
    [pendingData],
  )
  const activeOrders = useMemo(
    () => ordersData?.pages.flatMap((p) => p.data) ?? [],
    [ordersData],
  )

  const handleCancel = async () => {
    if (cancelId === null) return
    try {
      await cancelMutation.mutateAsync(cancelId)
      toast.success("Item cancelled.")
      setCancelId(null)
    } catch {
      toast.error("Failed to cancel item.")
    }
  }

  const isLoading = pendingLoading || ordersLoading

  if (isLoading) {
    return (
      <div className="space-y-4">
        {Array.from({ length: 3 }).map((_, i) => (
          <Card key={i}>
            <CardContent className="p-4">
              <div className="flex items-center gap-3">
                <Skeleton className="h-16 w-16 rounded" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-48" />
                  <Skeleton className="h-3 w-24" />
                </div>
                <Skeleton className="h-6 w-24" />
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    )
  }

  if (pendingItems.length === 0 && activeOrders.length === 0) {
    return (
      <div className="text-center py-12">
        <div className="inline-flex h-16 w-16 items-center justify-center rounded-full bg-muted mb-4">
          <Inbox className="h-8 w-8 text-muted-foreground" />
        </div>
        <h3 className="text-lg font-semibold mb-2">No pending orders</h3>
        <p className="text-muted-foreground mb-4">
          Items awaiting approval and active orders will appear here.
        </p>
        <Button asChild>
          <Link href="/">Start Shopping</Link>
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Pending Items (awaiting seller approval) */}
      {pendingItems.length > 0 && (
        <div className="space-y-3">
          <h3 className="text-sm font-medium text-muted-foreground">Awaiting Seller Approval</h3>
          <div className="space-y-3">
            {pendingItems.map((item) => (
              <PendingItemCard key={item.id} item={item} onCancel={setCancelId} />
            ))}
          </div>
          {hasMorePending && (
            <div className="text-center">
              <Button variant="outline" size="sm" onClick={() => fetchMorePending()} disabled={fetchingMorePending}>
                {fetchingMorePending ? <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Loading...</> : "Load More"}
              </Button>
            </div>
          )}
        </div>
      )}

      {/* Active Orders (unpaid / confirmed / shipped) */}
      {activeOrders.length > 0 && (
        <div className="space-y-3">
          {pendingItems.length > 0 && (
            <h3 className="text-sm font-medium text-muted-foreground">Active Orders</h3>
          )}
          <OrderList
            orders={activeOrders}
            hasNextPage={hasMoreOrders}
            isFetchingNextPage={fetchingMoreOrders}
            onLoadMore={() => fetchMoreOrders()}
          />
        </div>
      )}

      {/* Cancel Confirmation Dialog */}
      <Dialog open={cancelId !== null} onOpenChange={(open) => { if (!open) setCancelId(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Cancel Item</DialogTitle>
            <DialogDescription>
              Are you sure you want to cancel this item? This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCancelId(null)}>Keep Item</Button>
            <Button variant="destructive" onClick={handleCancel} disabled={cancelMutation.isPending}>
              {cancelMutation.isPending ? <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Cancelling...</> : "Cancel Item"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// ===== Main Page =====

export default function OrdersPage() {
  const {
    data: completedData,
    isLoading: completedLoading,
    fetchNextPage: fetchMoreCompleted,
    hasNextPage: hasMoreCompleted,
    isFetchingNextPage: fetchingMoreCompleted,
  } = useListBuyerConfirmed({ limit: 20, status: ["Delivered"] } as any)

  const {
    data: cancelledData,
    isLoading: cancelledLoading,
    fetchNextPage: fetchMoreCancelled,
    hasNextPage: hasMoreCancelled,
    isFetchingNextPage: fetchingMoreCancelled,
  } = useListBuyerConfirmed({ limit: 20, status: ["Cancelled"] } as any)

  const completedOrders = useMemo(
    () => completedData?.pages.flatMap((p) => p.data) ?? [],
    [completedData],
  )
  const cancelledOrders = useMemo(
    () => cancelledData?.pages.flatMap((p) => p.data) ?? [],
    [cancelledData],
  )

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">My Orders</h1>
        <p className="text-muted-foreground">View and track your orders</p>
      </div>

      <Tabs defaultValue="pending">
        <TabsList>
          <TabsTrigger value="pending">Pending</TabsTrigger>
          <TabsTrigger value="completed">Completed</TabsTrigger>
          <TabsTrigger value="cancelled">Cancelled</TabsTrigger>
        </TabsList>

        <TabsContent value="pending" className="mt-6">
          <PendingTab />
        </TabsContent>

        <TabsContent value="completed" className="mt-6">
          <OrderList
            orders={completedOrders}
            isLoading={completedLoading}
            hasNextPage={hasMoreCompleted}
            isFetchingNextPage={fetchingMoreCompleted}
            onLoadMore={() => fetchMoreCompleted()}
          />
        </TabsContent>

        <TabsContent value="cancelled" className="mt-6">
          <OrderList
            orders={cancelledOrders}
            isLoading={cancelledLoading}
            hasNextPage={hasMoreCancelled}
            isFetchingNextPage={fetchingMoreCancelled}
            onLoadMore={() => fetchMoreCancelled()}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}
```

- [ ] **Step 2: Verify the page renders**

Run: `cd ../shopnexus-website-v3 && bun dev`
Open: `http://localhost:5006/account/orders`
Expected: Three tabs visible — Pending (with item cards + order cards), Completed, Cancelled.

- [ ] **Step 3: Commit**

```bash
cd ../shopnexus-website-v3
git add app/\(marketplace\)/account/orders/page.tsx
git commit -m "rewrite buyer orders page with Pending/Completed/Cancelled tabs"
```

---

### Task 2: Rewrite seller orders page

**Files:**
- Modify: `../shopnexus-website-v3/app/(marketplace)/seller/orders/page.tsx`

This task moves the full `/seller/incoming` functionality into the Incoming tab, and keeps the existing confirmed orders list in the Confirmed tab.

- [ ] **Step 1: Replace the entire file with the new 2-tab implementation**

```tsx
"use client"

import { useState, useMemo } from "react"
import { useDebounceValue } from "usehooks-ts"
import Link from "next/link"
import Image from "next/image"
import { ProductLink } from "@/components/product/product-link"
import {
  useListSellerPending,
  useConfirmSellerPending,
  useRejectSellerPending,
  useListSellerConfirmed,
} from "@/core/order/order.seller"
import { TOrder, TOrderItem } from "@/core/order/order.buyer"
import { useListServiceOption } from "@/core/common/option"
import { useGetAccount } from "@/core/account/account"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Checkbox } from "@/components/ui/checkbox"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Textarea } from "@/components/ui/textarea"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Search,
  CheckCircle,
  XCircle,
  Package,
  Inbox,
  Loader2,
  MapPin,
  MoreVertical,
  Eye,
  ShoppingCart,
  Truck,
  Clock,
} from "lucide-react"
import { formatPrice, cn } from "@/lib/utils"
import { toast } from "sonner"

// ===== Shared Helpers =====

function AccountName({ id, fallback = "User" }: { id: string; fallback?: string }) {
  const { data } = useGetAccount(id)
  return <>{data?.name || data?.username || fallback}</>
}

function summarizeOrder(items?: Array<{ sku_name: string }>): string {
  if (!items?.length) return "Order"
  if (items.length === 1) return items[0].sku_name
  if (items.length === 2) return `${items[0].sku_name}, ${items[1].sku_name}`
  return `${items[0].sku_name} and ${items.length - 1} more`
}

const statusConfig: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline"; icon: React.ElementType }> = {
  Pending: { label: "Pending", variant: "secondary", icon: Clock },
  Confirmed: { label: "Confirmed", variant: "default", icon: CheckCircle },
  Shipped: { label: "Shipped", variant: "default", icon: Truck },
  Delivered: { label: "Delivered", variant: "outline", icon: Package },
  Cancelled: { label: "Cancelled", variant: "destructive", icon: XCircle },
}

// ===== Incoming Tab =====

function IncomingTab() {
  const [search, setSearch] = useState("")
  const [debouncedSearch] = useDebounceValue(search, 300)
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)
  const [showRejectDialog, setShowRejectDialog] = useState(false)
  const [transportOption, setTransportOption] = useState("")
  const [confirmNote, setConfirmNote] = useState("")

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useListSellerPending({
      limit: 20,
      ...(debouncedSearch ? { search: debouncedSearch } : {}),
    })
  const { data: transportOptions } = useListServiceOption({ category: "transport" })
  const confirmMutation = useConfirmSellerPending()
  const rejectMutation = useRejectSellerPending()

  const items = useMemo(
    () => data?.pages.flatMap((page) => page.data) ?? [],
    [data],
  )

  const grouped = useMemo(() => {
    const map = new Map<string, TOrderItem[]>()
    for (const item of items) {
      const key = `${item.account_id}::${item.address}`
      const existing = map.get(key) ?? []
      existing.push(item)
      map.set(key, existing)
    }
    return Array.from(map.entries())
  }, [items])

  const toggleItem = (id: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const selectAll = () => {
    if (selectedIds.size === items.length) {
      setSelectedIds(new Set())
    } else {
      setSelectedIds(new Set(items.map((i) => i.id)))
    }
  }

  const handleConfirm = async () => {
    if (!transportOption || selectedIds.size === 0) return
    try {
      await confirmMutation.mutateAsync({
        item_ids: Array.from(selectedIds),
        transport_option: transportOption,
        note: confirmNote || undefined,
      })
      toast.success("Items confirmed and order created.")
      setSelectedIds(new Set())
      setShowConfirmDialog(false)
      setTransportOption("")
      setConfirmNote("")
    } catch {
      toast.error("Failed to confirm items.")
    }
  }

  const handleReject = async () => {
    if (selectedIds.size === 0) return
    try {
      await rejectMutation.mutateAsync({ item_ids: Array.from(selectedIds) })
      toast.success("Items rejected.")
      setSelectedIds(new Set())
      setShowRejectDialog(false)
    } catch {
      toast.error("Failed to reject items.")
    }
  }

  return (
    <div className="space-y-4">
      {/* Search */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Search by SKU name, ID, or buyer..."
          className="pl-10 max-w-md"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>

      {/* Bulk Actions */}
      {items.length > 0 && (
        <div className="flex items-center gap-4">
          <Button variant="outline" size="sm" onClick={selectAll}>
            {selectedIds.size === items.length ? "Deselect All" : "Select All"}
          </Button>
          {selectedIds.size > 0 && (
            <>
              <span className="text-sm text-muted-foreground">
                {selectedIds.size} item{selectedIds.size !== 1 ? "s" : ""} selected
              </span>
              <Button size="sm" onClick={() => setShowConfirmDialog(true)}>
                <CheckCircle className="h-4 w-4 mr-2" />
                Confirm Selected
              </Button>
              <Button size="sm" variant="outline" onClick={() => setShowRejectDialog(true)}>
                <XCircle className="h-4 w-4 mr-2" />
                Reject Selected
              </Button>
            </>
          )}
        </div>
      )}

      {/* Items List */}
      {isLoading ? (
        <div className="space-y-4">
          {[...Array(5)].map((_, i) => (
            <Card key={i}>
              <CardContent className="p-4">
                <div className="space-y-3">
                  <div className="flex justify-between">
                    <Skeleton className="h-5 w-32" />
                    <Skeleton className="h-5 w-20" />
                  </div>
                  <Skeleton className="h-4 w-48" />
                  <Skeleton className="h-4 w-24" />
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : items.length === 0 ? (
        <Card>
          <CardContent className="p-8 text-center">
            <Inbox className="h-12 w-12 text-muted-foreground mx-auto mb-4" />
            <h3 className="text-lg font-semibold mb-2">No incoming items</h3>
            <p className="text-muted-foreground">
              {search ? "Try a different search term" : "New items from buyers will appear here"}
            </p>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-6">
          {grouped.map(([key, groupItems]) => {
            const [buyerId, address] = key.split("::")
            return (
              <Card key={key}>
                <CardContent className="p-4">
                  <div className="flex items-center gap-2 mb-4 pb-3 border-b">
                    <Badge variant="outline">
                      <AccountName id={buyerId} fallback="Buyer" />
                    </Badge>
                    <div className="flex items-center gap-1 text-sm text-muted-foreground">
                      <MapPin className="h-3 w-3" />
                      {address}
                    </div>
                  </div>

                  <div className="space-y-3">
                    {groupItems.map((item) => (
                      <div
                        key={item.id}
                        className={cn(
                          "flex items-center gap-3 p-3 rounded-lg border transition-colors min-h-28",
                          selectedIds.has(item.id) && "border-primary bg-accent/30",
                        )}
                      >
                        <Checkbox
                          checked={selectedIds.has(item.id)}
                          onCheckedChange={() => toggleItem(item.id)}
                          className="mt-1"
                        />
                        <div className="relative h-12 w-12 rounded bg-muted flex items-center justify-center flex-shrink-0">
                          {item.resources?.[0] ? (
                            <Image src={item.resources[0].url} alt={item.sku_name} fill className="object-cover rounded" />
                          ) : (
                            <Package className="h-5 w-5 text-muted-foreground" />
                          )}
                        </div>
                        <div className="flex-1 min-w-0">
                          <ProductLink spuId={item.spu_id} onClick={(e) => e.stopPropagation()}>
                            {item.sku_name}
                          </ProductLink>
                          {item.note && (
                            <p className="text-sm text-muted-foreground truncate">{item.note}</p>
                          )}
                          <div className="flex items-center gap-4 mt-1 text-sm">
                            <span>Qty: {item.quantity}</span>
                            <span className="font-medium">{formatPrice(item.unit_price)}/ea</span>
                            <span className="font-medium">{formatPrice(item.unit_price * item.quantity)} total</span>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            )
          })}

          {hasNextPage && (
            <div className="text-center pt-4">
              <Button variant="outline" onClick={() => fetchNextPage()} disabled={isFetchingNextPage}>
                {isFetchingNextPage ? <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Loading...</> : "Load More"}
              </Button>
            </div>
          )}
        </div>
      )}

      {/* Confirm Items Dialog */}
      <Dialog open={showConfirmDialog} onOpenChange={setShowConfirmDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm Items</DialogTitle>
            <DialogDescription>
              Confirm {selectedIds.size} selected item{selectedIds.size !== 1 ? "s" : ""} and create an order. Choose a transport option.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>Transport Option</Label>
              {transportOptions && transportOptions.length > 0 ? (
                <RadioGroup value={transportOption} onValueChange={setTransportOption} className="space-y-2">
                  {transportOptions.map((option) => (
                    <Label
                      key={option.id}
                      htmlFor={`transport-${option.id}`}
                      className={cn(
                        "flex items-center gap-3 rounded-lg border p-3 cursor-pointer transition-colors hover:bg-accent/50",
                        transportOption === option.id && "border-primary bg-accent/30",
                      )}
                    >
                      <RadioGroupItem value={option.id} id={`transport-${option.id}`} />
                      <div>
                        <span className="font-medium">{option.name}</span>
                        {option.description && <p className="text-sm text-muted-foreground">{option.description}</p>}
                      </div>
                    </Label>
                  ))}
                </RadioGroup>
              ) : (
                <p className="text-sm text-muted-foreground">No transport options available.</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="confirm-note">Note (optional)</Label>
              <Textarea
                id="confirm-note"
                placeholder="Add a note for the buyer..."
                value={confirmNote}
                onChange={(e) => setConfirmNote(e.target.value)}
              />
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowConfirmDialog(false)}>Cancel</Button>
            <Button onClick={handleConfirm} disabled={!transportOption || confirmMutation.isPending}>
              {confirmMutation.isPending ? (
                <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Confirming...</>
              ) : (
                <><CheckCircle className="h-4 w-4 mr-2" />Confirm Items</>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reject Items Dialog */}
      <Dialog open={showRejectDialog} onOpenChange={setShowRejectDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reject Items</DialogTitle>
            <DialogDescription>
              Are you sure you want to reject {selectedIds.size} selected item{selectedIds.size !== 1 ? "s" : ""}? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRejectDialog(false)}>Cancel</Button>
            <Button variant="destructive" onClick={handleReject} disabled={rejectMutation.isPending}>
              {rejectMutation.isPending ? (
                <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Rejecting...</>
              ) : (
                <><XCircle className="h-4 w-4 mr-2" />Reject Items</>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// ===== Confirmed Tab =====

function ConfirmedTab() {
  const [search, setSearch] = useState("")
  const [debouncedSearch] = useDebounceValue(search, 300)

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useListSellerConfirmed({
    limit: 20,
    ...(debouncedSearch ? { search: debouncedSearch } : {}),
  })

  const orders = useMemo(
    () => data?.pages.flatMap((page) => page.data) ?? [],
    [data],
  )

  const formatDate = (dateStr: string) =>
    new Date(dateStr).toLocaleDateString("en-US", {
      month: "short", day: "numeric", year: "numeric", hour: "2-digit", minute: "2-digit",
    })

  return (
    <div className="space-y-4">
      {/* Search */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Search by order ID..."
          className="pl-10 max-w-md"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>

      {/* Orders List */}
      {isLoading ? (
        <div className="space-y-4">
          {[...Array(5)].map((_, i) => (
            <Card key={i}>
              <CardContent className="p-4">
                <div className="space-y-3">
                  <div className="flex justify-between">
                    <Skeleton className="h-5 w-32" />
                    <Skeleton className="h-5 w-20" />
                  </div>
                  <Skeleton className="h-4 w-48" />
                  <Skeleton className="h-4 w-24" />
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : orders.length === 0 ? (
        <Card>
          <CardContent className="p-8 text-center">
            <ShoppingCart className="h-12 w-12 text-muted-foreground mx-auto mb-4" />
            <h3 className="text-lg font-semibold mb-2">No orders found</h3>
            <p className="text-muted-foreground">
              {search ? "Try a different search term" : "Orders will appear here when items are confirmed"}
            </p>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4">
          {orders.map((order) => {
            const status = statusConfig[order.status] ?? statusConfig.Pending
            const StatusIcon = status.icon

            return (
              <Card key={order.id}>
                <CardContent className="p-4">
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <h3 className="font-medium">{summarizeOrder(order.items)}</h3>
                        <Badge variant={status.variant} className="gap-1">
                          <StatusIcon className="h-3 w-3" />
                          {status.label}
                        </Badge>
                        {order.payment === null && (
                          <Badge variant="destructive" className="font-normal">Unpaid</Badge>
                        )}
                      </div>
                      <p className="text-sm text-muted-foreground">
                        #{order.id.slice(0, 8)} &middot; {formatDate(order.date_created)}
                      </p>
                      <p className="text-sm text-muted-foreground">
                        Buyer: <AccountName id={order.buyer_id} fallback="Buyer" />
                      </p>
                      <p className="text-sm">
                        {order.items.length} item{order.items.length !== 1 ? "s" : ""} |
                        <span className="font-medium ml-1">{formatPrice(order.total)}</span>
                      </p>
                    </div>

                    <div className="flex items-center gap-2">
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="outline" size="icon" className="h-8 w-8">
                            <MoreVertical className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem asChild>
                            <Link href={`/seller/orders/${order.id}`}>
                              <Eye className="h-4 w-4 mr-2" />
                              View Details
                            </Link>
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </div>

                  <div className="mt-4 pt-4 border-t">
                    <div className="flex gap-2 overflow-x-auto pb-2">
                      {order.items.slice(0, 4).map((item) => (
                        <div key={item.id} className="relative flex-shrink-0 h-12 w-12 rounded bg-muted flex items-center justify-center">
                          {item.resources?.[0] ? (
                            <Image src={item.resources[0].url} alt={item.sku_name} fill className="object-cover rounded" />
                          ) : (
                            <Package className="h-5 w-5 text-muted-foreground" />
                          )}
                        </div>
                      ))}
                      {order.items.length > 4 && (
                        <div className="flex-shrink-0 h-12 w-12 rounded bg-muted flex items-center justify-center text-sm text-muted-foreground">
                          +{order.items.length - 4}
                        </div>
                      )}
                    </div>
                  </div>
                </CardContent>
              </Card>
            )
          })}

          {hasNextPage && (
            <div className="text-center pt-4">
              <Button variant="outline" onClick={() => fetchNextPage()} disabled={isFetchingNextPage}>
                {isFetchingNextPage ? <><Loader2 className="h-4 w-4 mr-2 animate-spin" />Loading...</> : "Load More"}
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ===== Main Page =====

export default function SellerOrdersPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Orders</h1>
        <p className="text-muted-foreground">Manage incoming items and confirmed orders</p>
      </div>

      <Tabs defaultValue="incoming">
        <TabsList>
          <TabsTrigger value="incoming">Incoming</TabsTrigger>
          <TabsTrigger value="confirmed">Confirmed</TabsTrigger>
        </TabsList>

        <TabsContent value="incoming" className="mt-6">
          <IncomingTab />
        </TabsContent>

        <TabsContent value="confirmed" className="mt-6">
          <ConfirmedTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}
```

- [ ] **Step 2: Verify the page renders**

Run: `cd ../shopnexus-website-v3 && bun dev`
Open: `http://localhost:5006/seller/orders`
Expected: Two tabs — Incoming (with grouped items, checkboxes, bulk actions, confirm/reject dialogs) and Confirmed (with order cards, search, dropdown).

- [ ] **Step 3: Commit**

```bash
cd ../shopnexus-website-v3
git add app/\(marketplace\)/seller/orders/page.tsx
git commit -m "rewrite seller orders page with Incoming/Confirmed tabs"
```

---

### Task 3: Delete standalone pages

**Files:**
- Delete: `../shopnexus-website-v3/app/(marketplace)/account/pending-items/page.tsx`
- Delete: `../shopnexus-website-v3/app/(marketplace)/account/pending-items/_components/item-list.tsx`
- Delete: `../shopnexus-website-v3/app/(marketplace)/seller/incoming/page.tsx`

- [ ] **Step 1: Delete the files**

```bash
cd ../shopnexus-website-v3
rm app/\(marketplace\)/account/pending-items/page.tsx
rm app/\(marketplace\)/account/pending-items/_components/item-list.tsx
rmdir app/\(marketplace\)/account/pending-items/_components
rmdir app/\(marketplace\)/account/pending-items
rm app/\(marketplace\)/seller/incoming/page.tsx
rmdir app/\(marketplace\)/seller/incoming
```

- [ ] **Step 2: Search for dead references to removed pages**

Search for `/pending-items` and `/seller/incoming` links in the codebase. Expected references to clean up:
- `app/(marketplace)/account/orders/page.tsx` — already rewritten, no reference
- `app/(marketplace)/seller/orders/page.tsx` — already rewritten, no reference
- Any remaining `Link href="/account/pending-items"` or `Link href="/seller/incoming"` elsewhere

```bash
cd ../shopnexus-website-v3
grep -r "pending-items\|/seller/incoming" app/ --include="*.tsx" --include="*.ts" -l
```

Fix any remaining references (likely the notification href helper in `core/account/notification.ts` which maps `items_rejected` and `new_pending_items` to `/seller/incoming`). Update those to point to `/seller/orders` and `/account/orders` respectively.

- [ ] **Step 3: Update notification href helper**

In `../shopnexus-website-v3/core/account/notification.ts`, change:

```typescript
// Before
case 'items_rejected':
case 'new_pending_items':
case 'pending_item_cancelled':
  return '/seller/incoming'

// After
case 'items_rejected':
case 'new_pending_items':
case 'pending_item_cancelled':
  return '/seller/orders'
```

- [ ] **Step 4: Verify no broken links remain**

```bash
cd ../shopnexus-website-v3
grep -r "pending-items\|/seller/incoming" app/ core/ components/ --include="*.tsx" --include="*.ts" -l
```

Expected: No results (or only this plan file).

- [ ] **Step 5: Commit**

```bash
cd ../shopnexus-website-v3
git add -A
git commit -m "remove standalone pending-items and incoming pages, fix notification links"
```

---

### Task 4: Update useListBuyerConfirmed to support status filter

**Files:**
- Modify: `../shopnexus-website-v3/core/order/order.buyer.ts`

The buyer orders page passes `status: ["Pending", "Confirmed", "Shipped"]` to `useListBuyerConfirmed`, but the current hook's params type is `PaginationParams<unknown>` which doesn't type the status filter. The backend already supports `order_status` filter (same as seller). Update the type.

- [ ] **Step 1: Update the hook params type**

In `../shopnexus-website-v3/core/order/order.buyer.ts`, change:

```typescript
// Before
export const useListBuyerConfirmed = (params: PaginationParams<unknown>) =>
  useInfiniteQueryPagination<TOrder>(
    ['order', 'buyer', 'confirmed'],
    'order/buyer/confirmed',
    params
  )

// After
export const useListBuyerConfirmed = (params: PaginationParams<{
  status?: string[]
}>) =>
  useInfiniteQueryPagination<TOrder>(
    ['order', 'buyer', 'confirmed'],
    'order/buyer/confirmed',
    params
  )
```

- [ ] **Step 2: Remove the `as any` casts from the buyer orders page**

In `../shopnexus-website-v3/app/(marketplace)/account/orders/page.tsx`, remove the `as any` casts from the `useListBuyerConfirmed` calls (the type now accepts `status`).

- [ ] **Step 3: Commit**

```bash
cd ../shopnexus-website-v3
git add core/order/order.buyer.ts app/\(marketplace\)/account/orders/page.tsx
git commit -m "add status filter type to useListBuyerConfirmed"
```
