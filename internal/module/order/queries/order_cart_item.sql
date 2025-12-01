-- name: GetCartItem :one
SELECT *
FROM "order"."cart_item"
WHERE ("id" = sqlc.narg('id')) OR ("account_id" = sqlc.narg('account_id') AND "sku_id" = sqlc.narg('sku_id'));

-- name: CountCartItem :one
SELECT COUNT(*)
FROM "order"."cart_item"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("quantity" = ANY(sqlc.slice('quantity')) OR sqlc.slice('quantity') IS NULL) AND
    ("quantity" > sqlc.narg('quantity_from') OR sqlc.narg('quantity_from') IS NULL) AND
    ("quantity" < sqlc.narg('quantity_to') OR sqlc.narg('quantity_to') IS NULL)
);

-- name: ListCartItem :many
SELECT *
FROM "order"."cart_item"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("quantity" = ANY(sqlc.slice('quantity')) OR sqlc.slice('quantity') IS NULL) AND
    ("quantity" > sqlc.narg('quantity_from') OR sqlc.narg('quantity_from') IS NULL) AND
    ("quantity" < sqlc.narg('quantity_to') OR sqlc.narg('quantity_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountCartItem :many
SELECT sqlc.embed(embed_cart_item), COUNT(*) OVER() as total_count
FROM "order"."cart_item" embed_cart_item
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("quantity" = ANY(sqlc.slice('quantity')) OR sqlc.slice('quantity') IS NULL) AND
    ("quantity" > sqlc.narg('quantity_from') OR sqlc.narg('quantity_from') IS NULL) AND
    ("quantity" < sqlc.narg('quantity_to') OR sqlc.narg('quantity_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateCartItem :one
INSERT INTO "order"."cart_item" ("account_id", "sku_id", "quantity")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateBatchCartItem :batchone
INSERT INTO "order"."cart_item" ("account_id", "sku_id", "quantity")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCopyCartItem :copyfrom
INSERT INTO "order"."cart_item" ("account_id", "sku_id", "quantity")
VALUES ($1, $2, $3);

-- name: CreateDefaultCartItem :one
INSERT INTO "order"."cart_item" ("account_id", "sku_id", "quantity")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCopyDefaultCartItem :copyfrom
INSERT INTO "order"."cart_item" ("account_id", "sku_id", "quantity")
VALUES ($1, $2, $3);

-- name: UpdateCartItem :one
UPDATE "order"."cart_item"
SET "account_id" = COALESCE(sqlc.narg('account_id'), "account_id"),
    "sku_id" = COALESCE(sqlc.narg('sku_id'), "sku_id"),
    "quantity" = COALESCE(sqlc.narg('quantity'), "quantity")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteCartItem :exec
DELETE FROM "order"."cart_item"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("quantity" = ANY(sqlc.slice('quantity')) OR sqlc.slice('quantity') IS NULL) AND
    ("quantity" > sqlc.narg('quantity_from') OR sqlc.narg('quantity_from') IS NULL) AND
    ("quantity" < sqlc.narg('quantity_to') OR sqlc.narg('quantity_to') IS NULL)
);