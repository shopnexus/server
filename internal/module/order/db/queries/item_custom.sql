-- Custom item queries

-- name: CreateItem :one
INSERT INTO "order"."item" (
    "account_id", "seller_id", "sku_id", "sku_name",
    "address", "note", "serial_ids",
    "quantity", "transport_option",
    "subtotal_amount", "paid_amount", "payment_tx_id"
) VALUES (
    @account_id, @seller_id, @sku_id, @sku_name,
    @address, @note, @serial_ids,
    @quantity, @transport_option,
    @subtotal_amount, @paid_amount, @payment_tx_id
)
RETURNING *;

-- name: GetItem :one
SELECT * FROM "order"."item" WHERE "id" = @id;

-- name: ListItem :many
SELECT * FROM "order"."item" WHERE "id" = ANY(@id::BIGINT[]);

-- name: ListItemsByPaymentTx :many
SELECT * FROM "order"."item" WHERE "payment_tx_id" = @payment_tx_id;

-- name: ListSellerPendingItems :many
SELECT * FROM "order"."item"
WHERE "seller_id" = @seller_id
  AND "order_id" IS NULL
  AND "date_cancelled" IS NULL
ORDER BY "date_created" DESC;

-- name: CountSellerPendingItems :one
SELECT COUNT(*) FROM "order"."item"
WHERE "seller_id" = @seller_id
  AND "order_id" IS NULL
  AND "date_cancelled" IS NULL;

-- name: ListBuyerPendingItems :many
SELECT * FROM "order"."item"
WHERE "account_id" = @account_id
  AND "order_id" IS NULL
  AND "date_cancelled" IS NULL
ORDER BY "date_created" DESC;

-- name: CountBuyerPendingItems :one
SELECT COUNT(*) FROM "order"."item"
WHERE "account_id" = @account_id
  AND "order_id" IS NULL
  AND "date_cancelled" IS NULL;

-- name: ListPendingPaymentItemsByPaymentID :many
SELECT * FROM "order"."item"
WHERE "payment_tx_id" = @payment_tx_id
  AND "order_id" IS NULL
  AND "date_cancelled" IS NULL;

-- name: SetItemsOrderID :exec
UPDATE "order"."item"
SET "order_id" = @order_id
WHERE "id" = ANY(@item_ids::BIGINT[]) AND "order_id" IS NULL;

-- name: UnlinkItemsFromOrder :exec
UPDATE "order"."item"
SET "order_id" = NULL
WHERE "order_id" = @order_id;

-- name: CancelItem :one
UPDATE "order"."item"
SET "date_cancelled" = CURRENT_TIMESTAMP,
    "cancelled_by_id" = @cancelled_by_id,
    "refund_tx_id" = @refund_tx_id
WHERE "id" = @id AND "date_cancelled" IS NULL
RETURNING *;

-- name: CancelItemsByIDs :execrows
UPDATE "order"."item"
SET "date_cancelled" = CURRENT_TIMESTAMP
WHERE "id" = ANY(@item_ids::BIGINT[])
  AND "order_id" IS NULL
  AND "date_cancelled" IS NULL;

-- name: SumPaidAmountByOrder :one
SELECT COALESCE(SUM("paid_amount"), 0)::BIGINT AS total
FROM "order"."item"
WHERE "order_id" = @order_id AND "date_cancelled" IS NULL;
