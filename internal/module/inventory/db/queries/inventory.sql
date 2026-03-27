-- name: GetAvailableSerials :many
SELECT id, stock_id
FROM "inventory"."serial"
WHERE stock_id = sqlc.arg('stock_id') AND "status" = 'Active'
ORDER BY date_created DESC
FOR UPDATE SKIP LOCKED
LIMIT sqlc.arg('amount');

-- name: AdjustInventory :execrows
UPDATE inventory.stock
SET stock = stock - sqlc.arg('amount'),
    taken = taken + sqlc.arg('amount')
WHERE id = sqlc.arg('stock_id')
  AND stock >= sqlc.arg('amount');

-- name: UpdateSerialStatus :exec
UPDATE inventory.serial
SET status = sqlc.arg('status')
WHERE id = ANY(sqlc.slice('id'));

-- name: UpdateCurrentStock :exec
UPDATE "inventory"."stock"
SET "stock" = stock + sqlc.arg('change')
WHERE id = sqlc.arg('id');

-- name: ListMostTakenSku :many
SELECT *
FROM "inventory"."stock"
WHERE ref_type = sqlc.arg('ref_type')
ORDER BY taken DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ReleaseInventory :execrows
UPDATE "inventory"."stock"
SET "stock" = "stock" + @amount, "taken" = "taken" - @amount
WHERE "ref_id" = @ref_id AND "ref_type" = @ref_type AND "taken" >= @amount;
