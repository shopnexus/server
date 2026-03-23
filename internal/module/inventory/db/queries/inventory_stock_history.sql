-- name: GetStockHistory :one
SELECT *
FROM "inventory"."stock_history"
WHERE ("id" = sqlc.narg('id'));

-- name: ListCountStockHistory :many
SELECT sqlc.embed(embed_stock_history), COUNT(*) OVER() as total_count
FROM "inventory"."stock_history" embed_stock_history
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("stock_id" = ANY(sqlc.slice('stock_id')) OR sqlc.slice('stock_id') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateDefaultStockHistory :one
INSERT INTO "inventory"."stock_history" ("stock_id", "change")
VALUES ($1, $2)
RETURNING *;

-- name: DeleteStockHistory :exec
DELETE FROM "inventory"."stock_history"
WHERE ("id" = ANY(sqlc.slice('id')));
