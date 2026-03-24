-- name: UpsertProductPopularity :one
INSERT INTO "analytic"."product_popularity" ("id", "score", "view_count", "purchase_count", "favorite_count", "cart_count", "review_count")
VALUES (sqlc.arg('id'), sqlc.arg('score'), sqlc.arg('view_count'), sqlc.arg('purchase_count'), sqlc.arg('favorite_count'), sqlc.arg('cart_count'), sqlc.arg('review_count'))
ON CONFLICT ("id") DO UPDATE SET
    "score" = "analytic"."product_popularity"."score" + EXCLUDED."score",
    "view_count" = "analytic"."product_popularity"."view_count" + EXCLUDED."view_count",
    "purchase_count" = "analytic"."product_popularity"."purchase_count" + EXCLUDED."purchase_count",
    "favorite_count" = "analytic"."product_popularity"."favorite_count" + EXCLUDED."favorite_count",
    "cart_count" = "analytic"."product_popularity"."cart_count" + EXCLUDED."cart_count",
    "review_count" = "analytic"."product_popularity"."review_count" + EXCLUDED."review_count",
    "date_updated" = CURRENT_TIMESTAMP
RETURNING *;

-- name: GetProductPopularityByID :one
SELECT * FROM "analytic"."product_popularity"
WHERE "id" = sqlc.arg('id');

-- name: ListTopProductPopularity :many
SELECT * FROM "analytic"."product_popularity"
ORDER BY "score" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;