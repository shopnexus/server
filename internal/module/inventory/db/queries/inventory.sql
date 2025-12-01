-- name: GetAvailableSerials :many
SELECT id, sku_id
FROM "inventory"."sku_serial"
WHERE sku_id = sqlc.arg('sku_id') AND "status" = 'Active'
ORDER BY date_created DESC
FOR UPDATE SKIP LOCKED -- Lock the selected, but skip those already locked
LIMIT sqlc.arg('amount');

-- name: AdjustInventory :exec
UPDATE inventory.stock
SET stock = stock - sqlc.arg('amount'),
    taken = taken + sqlc.arg('amount')
WHERE ref_type = sqlc.arg('ref_type')
  AND ref_id = sqlc.arg('ref_id')
  AND stock >= sqlc.arg('amount');

-- name: UpdateSerialStatus :exec
UPDATE inventory.sku_serial
SET status = sqlc.arg('status')
WHERE id = ANY(sqlc.slice('id'));

-- name: UpdateCurrentStock :exec
UPDATE "inventory"."stock"
SET "stock" = stock + sqlc.arg('change')
WHERE id = sqlc.arg('id');
