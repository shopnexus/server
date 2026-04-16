-- Custom order queries

-- name: ListCountBuyerOrder :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() as total_count
FROM "order"."order" embed_order
WHERE embed_order."buyer_id" = @buyer_id
    AND (embed_order."id"::text ILIKE '%' || sqlc.narg('search')::text || '%' OR sqlc.narg('search') IS NULL)
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountSellerOrder :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() as total_count
FROM "order"."order" embed_order
WHERE embed_order."seller_id" = @seller_id
    AND (embed_order."id"::text ILIKE '%' || sqlc.narg('search')::text || '%' OR sqlc.narg('search') IS NULL)
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: HasPurchasedSku :one
SELECT EXISTS(
    SELECT 1 FROM "order".item i
    WHERE i.account_id = @account_id
      AND i.order_id IS NOT NULL
      AND i.date_cancelled IS NULL
      AND i.sku_id = ANY(@sku_ids::UUID[])
) AS has_purchased;

-- name: ListSuccessOrdersBySkus :many
SELECT DISTINCT o.* FROM "order"."order" o
JOIN "order".item i ON i.order_id = o.id
WHERE o.buyer_id = @buyer_id
  AND i.sku_id = ANY(@sku_ids::UUID[])
  AND i.date_cancelled IS NULL
ORDER BY o.date_created DESC;

-- name: ValidateOrderForReview :one
SELECT EXISTS(
    SELECT 1 FROM "order"."order" o
    JOIN "order".item i ON i.order_id = o.id
    WHERE o.id = @order_id
      AND o.buyer_id = @buyer_id
      AND i.sku_id = ANY(@sku_ids::UUID[])
      AND i.date_cancelled IS NULL
) AS is_valid;
