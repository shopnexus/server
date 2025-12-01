-- name: GetSkuSerial :one
SELECT *
FROM "inventory"."sku_serial"
WHERE ("id" = sqlc.narg('id'));

-- name: CountSkuSerial :one
SELECT COUNT(*)
FROM "inventory"."sku_serial"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);

-- name: ListSkuSerial :many
SELECT *
FROM "inventory"."sku_serial"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountSkuSerial :many
SELECT sqlc.embed(embed_sku_serial), COUNT(*) OVER() as total_count
FROM "inventory"."sku_serial" embed_sku_serial
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateSkuSerial :one
INSERT INTO "inventory"."sku_serial" ("id", "sku_id", "status", "date_created")
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateBatchSkuSerial :batchone
INSERT INTO "inventory"."sku_serial" ("id", "sku_id", "status", "date_created")
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateCopySkuSerial :copyfrom
INSERT INTO "inventory"."sku_serial" ("id", "sku_id", "status", "date_created")
VALUES ($1, $2, $3, $4);

-- name: CreateDefaultSkuSerial :one
INSERT INTO "inventory"."sku_serial" ("id", "sku_id")
VALUES ($1, $2)
RETURNING *;

-- name: CreateCopyDefaultSkuSerial :copyfrom
INSERT INTO "inventory"."sku_serial" ("id", "sku_id")
VALUES ($1, $2);

-- name: UpdateSkuSerial :one
UPDATE "inventory"."sku_serial"
SET "sku_id" = COALESCE(sqlc.narg('sku_id'), "sku_id"),
    "status" = COALESCE(sqlc.narg('status'), "status"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteSkuSerial :exec
DELETE FROM "inventory"."sku_serial"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);