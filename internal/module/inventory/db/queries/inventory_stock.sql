-- name: GetStock :one
SELECT *
FROM "inventory"."stock"
WHERE ("id" = sqlc.narg('id')) OR ("ref_id" = sqlc.narg('ref_id') AND "ref_type" = sqlc.narg('ref_type'));

-- name: ListCountStock :many
SELECT sqlc.embed(embed_stock), COUNT(*) OVER() as total_count
FROM "inventory"."stock" embed_stock
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateDefaultStock :one
INSERT INTO "inventory"."stock" ("ref_type", "ref_id", "stock")
VALUES ($1, $2, $3)
RETURNING *;

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
WHERE ("id" = ANY(sqlc.slice('id')));
