-- Custom item queries

-- name: ListPendingItemsBySeller :many
SELECT *
FROM "order"."item"
WHERE "seller_id" = @seller_id AND "status" = 'Pending'
    AND ("sku_name" ILIKE '%' || sqlc.narg('search')::text || '%' OR sqlc.narg('search') IS NULL)
ORDER BY "date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CountPendingItemsBySeller :one
SELECT COUNT(*)
FROM "order"."item"
WHERE "seller_id" = @seller_id AND "status" = 'Pending'
    AND ("sku_name" ILIKE '%' || sqlc.narg('search')::text || '%' OR sqlc.narg('search') IS NULL);

-- name: ListPendingItemsByAccount :many
SELECT *
FROM "order"."item"
WHERE "account_id" = @account_id AND "status" = ANY(@status::"order".item_status[])
ORDER BY "date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CountPendingItemsByAccount :one
SELECT COUNT(*)
FROM "order"."item"
WHERE "account_id" = @account_id AND "status" = ANY(@status::"order".item_status[]);

-- name: ConfirmItems :exec
UPDATE "order"."item"
SET "order_id" = @order_id, "status" = 'Confirmed', "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = ANY(@ids::bigint[]) AND "status" = 'Pending';

-- name: CancelItem :exec
UPDATE "order"."item"
SET "status" = 'Cancelled', "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = @id AND "account_id" = @account_id AND "status" = 'Pending';

-- name: CancelItemsBySeller :exec
UPDATE "order"."item"
SET "status" = 'Cancelled', "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = ANY(@ids::bigint[]) AND "seller_id" = @seller_id AND "status" = 'Pending';

-- name: CancelItemsByOrder :exec
UPDATE "order"."item"
SET "status" = 'Cancelled', "date_updated" = CURRENT_TIMESTAMP
WHERE "order_id" = @order_id;

-- name: CreatePendingItem :one
INSERT INTO "order"."item" (
    "account_id", "seller_id", "address", "status",
    "sku_id", "sku_name", "quantity", "unit_price", "paid_amount",
    "note", "serial_ids"
)
VALUES ($1, $2, $3, 'Pending', $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: HasPurchasedSku :one
SELECT EXISTS(
    SELECT 1
    FROM "order"."item" i
    JOIN "order"."order" o ON i."order_id" = o."id"
    JOIN "order"."payment" p ON o."payment_id" = p."id"
    WHERE i."account_id" = @account_id
      AND i."sku_id" = ANY(@sku_ids::uuid[])
      AND p."status" = 'Success'
) AS has_purchased;

-- name: ListSuccessOrdersBySkus :many
SELECT DISTINCT o.id, o.buyer_id, o.seller_id, o.payment_id, o.transport_id, o.confirmed_by_id, o.address, o.product_cost, o.product_discount, o.transport_cost, o.total, o.note, o.data, o.date_created
FROM "order"."order" o
JOIN "order"."item" i ON i."order_id" = o."id"
JOIN "order"."payment" p ON o."payment_id" = p."id"
WHERE o."buyer_id" = @buyer_id
  AND i."sku_id" = ANY(@sku_ids::uuid[])
  AND p."status" = 'Success'
ORDER BY o."date_created" DESC;

-- name: ValidateOrderForReview :one
SELECT EXISTS(
    SELECT 1
    FROM "order"."order" o
    JOIN "order"."item" i ON i."order_id" = o."id"
    JOIN "order"."payment" p ON o."payment_id" = p."id"
    WHERE o."id" = @order_id
      AND o."buyer_id" = @buyer_id
      AND i."sku_id" = ANY(@sku_ids::uuid[])
      AND p."status" = 'Success'
) AS is_valid;
