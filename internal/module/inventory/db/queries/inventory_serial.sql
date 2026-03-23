-- name: GetSerial :one
SELECT *
FROM "inventory"."serial"
WHERE ("id" = sqlc.narg('id'));

-- name: ListCountSerial :many
SELECT sqlc.embed(embed_serial), COUNT(*) OVER() as total_count
FROM "inventory"."serial" embed_serial
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("stock_id" = ANY(sqlc.slice('stock_id')) OR sqlc.slice('stock_id') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateCopyDefaultSerial :copyfrom
INSERT INTO "inventory"."serial" ("id", "stock_id")
VALUES ($1, $2);

-- name: UpdateSerial :one
UPDATE "inventory"."serial"
SET "stock_id" = COALESCE(sqlc.narg('stock_id'), "stock_id"),
    "status" = COALESCE(sqlc.narg('status'), "status"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteSerial :exec
DELETE FROM "inventory"."serial"
WHERE ("id" = ANY(sqlc.slice('id')));
