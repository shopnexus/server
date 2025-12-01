-- name: GetDiscount :one
SELECT *
FROM "promotion"."discount"
WHERE ("id" = sqlc.narg('id'));

-- name: CountDiscount :one
SELECT COUNT(*)
FROM "promotion"."discount"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("min_spend" = ANY(sqlc.slice('min_spend')) OR sqlc.slice('min_spend') IS NULL) AND
    ("min_spend" > sqlc.narg('min_spend_from') OR sqlc.narg('min_spend_from') IS NULL) AND
    ("min_spend" < sqlc.narg('min_spend_to') OR sqlc.narg('min_spend_to') IS NULL) AND
    ("max_discount" = ANY(sqlc.slice('max_discount')) OR sqlc.slice('max_discount') IS NULL) AND
    ("max_discount" > sqlc.narg('max_discount_from') OR sqlc.narg('max_discount_from') IS NULL) AND
    ("max_discount" < sqlc.narg('max_discount_to') OR sqlc.narg('max_discount_to') IS NULL) AND
    ("discount_percent" = ANY(sqlc.slice('discount_percent')) OR sqlc.slice('discount_percent') IS NULL) AND
    ("discount_percent" > sqlc.narg('discount_percent_from') OR sqlc.narg('discount_percent_from') IS NULL) AND
    ("discount_percent" < sqlc.narg('discount_percent_to') OR sqlc.narg('discount_percent_to') IS NULL) AND
    ("discount_price" = ANY(sqlc.slice('discount_price')) OR sqlc.slice('discount_price') IS NULL) AND
    ("discount_price" > sqlc.narg('discount_price_from') OR sqlc.narg('discount_price_from') IS NULL) AND
    ("discount_price" < sqlc.narg('discount_price_to') OR sqlc.narg('discount_price_to') IS NULL)
);

-- name: ListDiscount :many
SELECT *
FROM "promotion"."discount"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("min_spend" = ANY(sqlc.slice('min_spend')) OR sqlc.slice('min_spend') IS NULL) AND
    ("min_spend" > sqlc.narg('min_spend_from') OR sqlc.narg('min_spend_from') IS NULL) AND
    ("min_spend" < sqlc.narg('min_spend_to') OR sqlc.narg('min_spend_to') IS NULL) AND
    ("max_discount" = ANY(sqlc.slice('max_discount')) OR sqlc.slice('max_discount') IS NULL) AND
    ("max_discount" > sqlc.narg('max_discount_from') OR sqlc.narg('max_discount_from') IS NULL) AND
    ("max_discount" < sqlc.narg('max_discount_to') OR sqlc.narg('max_discount_to') IS NULL) AND
    ("discount_percent" = ANY(sqlc.slice('discount_percent')) OR sqlc.slice('discount_percent') IS NULL) AND
    ("discount_percent" > sqlc.narg('discount_percent_from') OR sqlc.narg('discount_percent_from') IS NULL) AND
    ("discount_percent" < sqlc.narg('discount_percent_to') OR sqlc.narg('discount_percent_to') IS NULL) AND
    ("discount_price" = ANY(sqlc.slice('discount_price')) OR sqlc.slice('discount_price') IS NULL) AND
    ("discount_price" > sqlc.narg('discount_price_from') OR sqlc.narg('discount_price_from') IS NULL) AND
    ("discount_price" < sqlc.narg('discount_price_to') OR sqlc.narg('discount_price_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountDiscount :many
SELECT sqlc.embed(embed_discount), COUNT(*) OVER() as total_count
FROM "promotion"."discount" embed_discount
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("min_spend" = ANY(sqlc.slice('min_spend')) OR sqlc.slice('min_spend') IS NULL) AND
    ("min_spend" > sqlc.narg('min_spend_from') OR sqlc.narg('min_spend_from') IS NULL) AND
    ("min_spend" < sqlc.narg('min_spend_to') OR sqlc.narg('min_spend_to') IS NULL) AND
    ("max_discount" = ANY(sqlc.slice('max_discount')) OR sqlc.slice('max_discount') IS NULL) AND
    ("max_discount" > sqlc.narg('max_discount_from') OR sqlc.narg('max_discount_from') IS NULL) AND
    ("max_discount" < sqlc.narg('max_discount_to') OR sqlc.narg('max_discount_to') IS NULL) AND
    ("discount_percent" = ANY(sqlc.slice('discount_percent')) OR sqlc.slice('discount_percent') IS NULL) AND
    ("discount_percent" > sqlc.narg('discount_percent_from') OR sqlc.narg('discount_percent_from') IS NULL) AND
    ("discount_percent" < sqlc.narg('discount_percent_to') OR sqlc.narg('discount_percent_to') IS NULL) AND
    ("discount_price" = ANY(sqlc.slice('discount_price')) OR sqlc.slice('discount_price') IS NULL) AND
    ("discount_price" > sqlc.narg('discount_price_from') OR sqlc.narg('discount_price_from') IS NULL) AND
    ("discount_price" < sqlc.narg('discount_price_to') OR sqlc.narg('discount_price_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateDiscount :one
INSERT INTO "promotion"."discount" ("id", "min_spend", "max_discount", "discount_percent", "discount_price")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateBatchDiscount :batchone
INSERT INTO "promotion"."discount" ("id", "min_spend", "max_discount", "discount_percent", "discount_price")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateCopyDiscount :copyfrom
INSERT INTO "promotion"."discount" ("id", "min_spend", "max_discount", "discount_percent", "discount_price")
VALUES ($1, $2, $3, $4, $5);

-- name: CreateDefaultDiscount :one
INSERT INTO "promotion"."discount" ("id", "min_spend", "max_discount", "discount_percent", "discount_price")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateCopyDefaultDiscount :copyfrom
INSERT INTO "promotion"."discount" ("id", "min_spend", "max_discount", "discount_percent", "discount_price")
VALUES ($1, $2, $3, $4, $5);

-- name: UpdateDiscount :one
UPDATE "promotion"."discount"
SET "min_spend" = COALESCE(sqlc.narg('min_spend'), "min_spend"),
    "max_discount" = COALESCE(sqlc.narg('max_discount'), "max_discount"),
    "discount_percent" = CASE WHEN sqlc.arg('null_discount_percent')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('discount_percent'), "discount_percent") END,
    "discount_price" = CASE WHEN sqlc.arg('null_discount_price')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('discount_price'), "discount_price") END
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteDiscount :exec
DELETE FROM "promotion"."discount"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("min_spend" = ANY(sqlc.slice('min_spend')) OR sqlc.slice('min_spend') IS NULL) AND
    ("min_spend" > sqlc.narg('min_spend_from') OR sqlc.narg('min_spend_from') IS NULL) AND
    ("min_spend" < sqlc.narg('min_spend_to') OR sqlc.narg('min_spend_to') IS NULL) AND
    ("max_discount" = ANY(sqlc.slice('max_discount')) OR sqlc.slice('max_discount') IS NULL) AND
    ("max_discount" > sqlc.narg('max_discount_from') OR sqlc.narg('max_discount_from') IS NULL) AND
    ("max_discount" < sqlc.narg('max_discount_to') OR sqlc.narg('max_discount_to') IS NULL) AND
    ("discount_percent" = ANY(sqlc.slice('discount_percent')) OR sqlc.slice('discount_percent') IS NULL) AND
    ("discount_percent" > sqlc.narg('discount_percent_from') OR sqlc.narg('discount_percent_from') IS NULL) AND
    ("discount_percent" < sqlc.narg('discount_percent_to') OR sqlc.narg('discount_percent_to') IS NULL) AND
    ("discount_price" = ANY(sqlc.slice('discount_price')) OR sqlc.slice('discount_price') IS NULL) AND
    ("discount_price" > sqlc.narg('discount_price_from') OR sqlc.narg('discount_price_from') IS NULL) AND
    ("discount_price" < sqlc.narg('discount_price_to') OR sqlc.narg('discount_price_to') IS NULL)
);