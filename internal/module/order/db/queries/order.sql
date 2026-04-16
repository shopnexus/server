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
