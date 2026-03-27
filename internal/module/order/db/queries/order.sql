-- Custom order queries

-- name: ListCountSellerOrder :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() as total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment" p ON embed_order."payment_id" = p."id"
WHERE embed_order."seller_id" = @seller_id
    AND (p."status" = ANY(sqlc.slice('payment_status')) OR sqlc.slice('payment_status') IS NULL)
    AND (embed_order."status" = ANY(sqlc.slice('order_status')) OR sqlc.slice('order_status') IS NULL)
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: SetOrderPayment :exec
UPDATE "order"."order"
SET "payment_id" = @payment_id
WHERE "id" = ANY(@ids::uuid[]) AND "buyer_id" = @buyer_id AND "payment_id" IS NULL;
