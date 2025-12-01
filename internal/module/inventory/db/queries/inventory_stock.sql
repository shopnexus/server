-- name: GetStock :one
SELECT *
FROM "inventory"."stock"
WHERE ("id" = sqlc.narg('id')) OR ("ref_id" = sqlc.narg('ref_id') AND "ref_type" = sqlc.narg('ref_type'));

-- name: CountStock :one
SELECT COUNT(*)
FROM "inventory"."stock"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("stock" = ANY(sqlc.slice('stock')) OR sqlc.slice('stock') IS NULL) AND
    ("stock" > sqlc.narg('stock_from') OR sqlc.narg('stock_from') IS NULL) AND
    ("stock" < sqlc.narg('stock_to') OR sqlc.narg('stock_to') IS NULL) AND
    ("taken" = ANY(sqlc.slice('taken')) OR sqlc.slice('taken') IS NULL) AND
    ("taken" > sqlc.narg('taken_from') OR sqlc.narg('taken_from') IS NULL) AND
    ("taken" < sqlc.narg('taken_to') OR sqlc.narg('taken_to') IS NULL) AND
    ("serial_required" = ANY(sqlc.slice('serial_required')) OR sqlc.slice('serial_required') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);

-- name: ListStock :many
SELECT *
FROM "inventory"."stock"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("stock" = ANY(sqlc.slice('stock')) OR sqlc.slice('stock') IS NULL) AND
    ("stock" > sqlc.narg('stock_from') OR sqlc.narg('stock_from') IS NULL) AND
    ("stock" < sqlc.narg('stock_to') OR sqlc.narg('stock_to') IS NULL) AND
    ("taken" = ANY(sqlc.slice('taken')) OR sqlc.slice('taken') IS NULL) AND
    ("taken" > sqlc.narg('taken_from') OR sqlc.narg('taken_from') IS NULL) AND
    ("taken" < sqlc.narg('taken_to') OR sqlc.narg('taken_to') IS NULL) AND
    ("serial_required" = ANY(sqlc.slice('serial_required')) OR sqlc.slice('serial_required') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountStock :many
SELECT sqlc.embed(embed_stock), COUNT(*) OVER() as total_count
FROM "inventory"."stock" embed_stock
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("stock" = ANY(sqlc.slice('stock')) OR sqlc.slice('stock') IS NULL) AND
    ("stock" > sqlc.narg('stock_from') OR sqlc.narg('stock_from') IS NULL) AND
    ("stock" < sqlc.narg('stock_to') OR sqlc.narg('stock_to') IS NULL) AND
    ("taken" = ANY(sqlc.slice('taken')) OR sqlc.slice('taken') IS NULL) AND
    ("taken" > sqlc.narg('taken_from') OR sqlc.narg('taken_from') IS NULL) AND
    ("taken" < sqlc.narg('taken_to') OR sqlc.narg('taken_to') IS NULL) AND
    ("serial_required" = ANY(sqlc.slice('serial_required')) OR sqlc.slice('serial_required') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateStock :one
INSERT INTO "inventory"."stock" ("ref_type", "ref_id", "stock", "taken", "serial_required", "date_created")
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateBatchStock :batchone
INSERT INTO "inventory"."stock" ("ref_type", "ref_id", "stock", "taken", "serial_required", "date_created")
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateCopyStock :copyfrom
INSERT INTO "inventory"."stock" ("ref_type", "ref_id", "stock", "taken", "serial_required", "date_created")
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CreateDefaultStock :one
INSERT INTO "inventory"."stock" ("ref_type", "ref_id", "stock")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCopyDefaultStock :copyfrom
INSERT INTO "inventory"."stock" ("ref_type", "ref_id", "stock")
VALUES ($1, $2, $3);

-- name: UpdateStock :one
UPDATE "inventory"."stock"
SET "ref_type" = COALESCE(sqlc.narg('ref_type'), "ref_type"),
    "ref_id" = COALESCE(sqlc.narg('ref_id'), "ref_id"),
    "stock" = COALESCE(sqlc.narg('stock'), "stock"),
    "taken" = COALESCE(sqlc.narg('taken'), "taken"),
    "serial_required" = COALESCE(sqlc.narg('serial_required'), "serial_required"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteStock :exec
DELETE FROM "inventory"."stock"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("stock" = ANY(sqlc.slice('stock')) OR sqlc.slice('stock') IS NULL) AND
    ("stock" > sqlc.narg('stock_from') OR sqlc.narg('stock_from') IS NULL) AND
    ("stock" < sqlc.narg('stock_to') OR sqlc.narg('stock_to') IS NULL) AND
    ("taken" = ANY(sqlc.slice('taken')) OR sqlc.slice('taken') IS NULL) AND
    ("taken" > sqlc.narg('taken_from') OR sqlc.narg('taken_from') IS NULL) AND
    ("taken" < sqlc.narg('taken_to') OR sqlc.narg('taken_to') IS NULL) AND
    ("serial_required" = ANY(sqlc.slice('serial_required')) OR sqlc.slice('serial_required') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);