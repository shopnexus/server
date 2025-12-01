-- name: GetItem :one
SELECT *
FROM "order"."item"
WHERE ("id" = sqlc.narg('id')) OR ("order_id" = sqlc.narg('order_id') AND "sku_id" = sqlc.narg('sku_id'));

-- name: CountItem :one
SELECT COUNT(*)
FROM "order"."item"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("order_id" = ANY(sqlc.slice('order_id')) OR sqlc.slice('order_id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("sku_name" = ANY(sqlc.slice('sku_name')) OR sqlc.slice('sku_name') IS NULL) AND
    ("quantity" = ANY(sqlc.slice('quantity')) OR sqlc.slice('quantity') IS NULL) AND
    ("quantity" > sqlc.narg('quantity_from') OR sqlc.narg('quantity_from') IS NULL) AND
    ("quantity" < sqlc.narg('quantity_to') OR sqlc.narg('quantity_to') IS NULL) AND
    ("unit_price" = ANY(sqlc.slice('unit_price')) OR sqlc.slice('unit_price') IS NULL) AND
    ("unit_price" > sqlc.narg('unit_price_from') OR sqlc.narg('unit_price_from') IS NULL) AND
    ("unit_price" < sqlc.narg('unit_price_to') OR sqlc.narg('unit_price_to') IS NULL) AND
    ("note" = ANY(sqlc.slice('note')) OR sqlc.slice('note') IS NULL) AND
    ("serial_ids" = ANY(sqlc.slice('serial_ids')) OR sqlc.slice('serial_ids') IS NULL)
);

-- name: ListItem :many
SELECT *
FROM "order"."item"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("order_id" = ANY(sqlc.slice('order_id')) OR sqlc.slice('order_id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("sku_name" = ANY(sqlc.slice('sku_name')) OR sqlc.slice('sku_name') IS NULL) AND
    ("quantity" = ANY(sqlc.slice('quantity')) OR sqlc.slice('quantity') IS NULL) AND
    ("quantity" > sqlc.narg('quantity_from') OR sqlc.narg('quantity_from') IS NULL) AND
    ("quantity" < sqlc.narg('quantity_to') OR sqlc.narg('quantity_to') IS NULL) AND
    ("unit_price" = ANY(sqlc.slice('unit_price')) OR sqlc.slice('unit_price') IS NULL) AND
    ("unit_price" > sqlc.narg('unit_price_from') OR sqlc.narg('unit_price_from') IS NULL) AND
    ("unit_price" < sqlc.narg('unit_price_to') OR sqlc.narg('unit_price_to') IS NULL) AND
    ("note" = ANY(sqlc.slice('note')) OR sqlc.slice('note') IS NULL) AND
    ("serial_ids" = ANY(sqlc.slice('serial_ids')) OR sqlc.slice('serial_ids') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountItem :many
SELECT sqlc.embed(embed_item), COUNT(*) OVER() as total_count
FROM "order"."item" embed_item
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("order_id" = ANY(sqlc.slice('order_id')) OR sqlc.slice('order_id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("sku_name" = ANY(sqlc.slice('sku_name')) OR sqlc.slice('sku_name') IS NULL) AND
    ("quantity" = ANY(sqlc.slice('quantity')) OR sqlc.slice('quantity') IS NULL) AND
    ("quantity" > sqlc.narg('quantity_from') OR sqlc.narg('quantity_from') IS NULL) AND
    ("quantity" < sqlc.narg('quantity_to') OR sqlc.narg('quantity_to') IS NULL) AND
    ("unit_price" = ANY(sqlc.slice('unit_price')) OR sqlc.slice('unit_price') IS NULL) AND
    ("unit_price" > sqlc.narg('unit_price_from') OR sqlc.narg('unit_price_from') IS NULL) AND
    ("unit_price" < sqlc.narg('unit_price_to') OR sqlc.narg('unit_price_to') IS NULL) AND
    ("note" = ANY(sqlc.slice('note')) OR sqlc.slice('note') IS NULL) AND
    ("serial_ids" = ANY(sqlc.slice('serial_ids')) OR sqlc.slice('serial_ids') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateItem :one
INSERT INTO "order"."item" ("order_id", "sku_id", "sku_name", "quantity", "unit_price", "note", "serial_ids")
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: CreateBatchItem :batchone
INSERT INTO "order"."item" ("order_id", "sku_id", "sku_name", "quantity", "unit_price", "note", "serial_ids")
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: CreateCopyItem :copyfrom
INSERT INTO "order"."item" ("order_id", "sku_id", "sku_name", "quantity", "unit_price", "note", "serial_ids")
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: CreateDefaultItem :one
INSERT INTO "order"."item" ("order_id", "sku_id", "sku_name", "quantity", "unit_price", "note", "serial_ids")
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: CreateCopyDefaultItem :copyfrom
INSERT INTO "order"."item" ("order_id", "sku_id", "sku_name", "quantity", "unit_price", "note", "serial_ids")
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: UpdateItem :one
UPDATE "order"."item"
SET "order_id" = COALESCE(sqlc.narg('order_id'), "order_id"),
    "sku_id" = COALESCE(sqlc.narg('sku_id'), "sku_id"),
    "sku_name" = COALESCE(sqlc.narg('sku_name'), "sku_name"),
    "quantity" = COALESCE(sqlc.narg('quantity'), "quantity"),
    "unit_price" = COALESCE(sqlc.narg('unit_price'), "unit_price"),
    "note" = CASE WHEN sqlc.arg('null_note')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('note'), "note") END,
    "serial_ids" = CASE WHEN sqlc.arg('null_serial_ids')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('serial_ids'), "serial_ids") END
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteItem :exec
DELETE FROM "order"."item"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("order_id" = ANY(sqlc.slice('order_id')) OR sqlc.slice('order_id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("sku_name" = ANY(sqlc.slice('sku_name')) OR sqlc.slice('sku_name') IS NULL) AND
    ("quantity" = ANY(sqlc.slice('quantity')) OR sqlc.slice('quantity') IS NULL) AND
    ("quantity" > sqlc.narg('quantity_from') OR sqlc.narg('quantity_from') IS NULL) AND
    ("quantity" < sqlc.narg('quantity_to') OR sqlc.narg('quantity_to') IS NULL) AND
    ("unit_price" = ANY(sqlc.slice('unit_price')) OR sqlc.slice('unit_price') IS NULL) AND
    ("unit_price" > sqlc.narg('unit_price_from') OR sqlc.narg('unit_price_from') IS NULL) AND
    ("unit_price" < sqlc.narg('unit_price_to') OR sqlc.narg('unit_price_to') IS NULL) AND
    ("note" = ANY(sqlc.slice('note')) OR sqlc.slice('note') IS NULL) AND
    ("serial_ids" = ANY(sqlc.slice('serial_ids')) OR sqlc.slice('serial_ids') IS NULL)
);