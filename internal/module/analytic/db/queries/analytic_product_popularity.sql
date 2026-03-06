-- name: GetProductPopularity :one
SELECT *
FROM "analytic"."product_popularity"
WHERE "spu_id" = sqlc.arg('spu_id');

-- name: ListProductPopularity :many
SELECT *
FROM "analytic"."product_popularity"
WHERE "spu_id" = ANY(sqlc.slice('spu_id'))
ORDER BY "score" DESC;

-- name: ListTopProductPopularity :many
SELECT *
FROM "analytic"."product_popularity"
ORDER BY "score" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: UpsertProductPopularity :one
INSERT INTO "analytic"."product_popularity" ("spu_id", "score", "view_count", "purchase_count", "favorite_count", "cart_count", "review_count", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
ON CONFLICT ("spu_id") DO UPDATE
SET "score" = "analytic"."product_popularity"."score" + EXCLUDED."score",
    "view_count" = "analytic"."product_popularity"."view_count" + EXCLUDED."view_count",
    "purchase_count" = "analytic"."product_popularity"."purchase_count" + EXCLUDED."purchase_count",
    "favorite_count" = "analytic"."product_popularity"."favorite_count" + EXCLUDED."favorite_count",
    "cart_count" = "analytic"."product_popularity"."cart_count" + EXCLUDED."cart_count",
    "review_count" = "analytic"."product_popularity"."review_count" + EXCLUDED."review_count",
    "date_updated" = CURRENT_TIMESTAMP
RETURNING *;
