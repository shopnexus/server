-- name: GetAvailableProducts :batchmany
SELECT id, sku_id, serial_number
FROM "inventory"."sku_serial"
WHERE sku_id = sqlc.arg('sku_id') AND "status" = 'Active'
ORDER BY date_created DESC
FOR UPDATE SKIP LOCKED -- Lock the selected, but skip those already locked
LIMIT sqlc.arg('amount');

-- name: ReserveInventory :batchexec
UPDATE inventory.stock
SET current_stock = current_stock - sqlc.arg('amount'),
    sold = sold + sqlc.arg('amount')
WHERE ref_type = sqlc.arg('ref_type')
  AND ref_id = sqlc.arg('ref_id')
  AND current_stock >= sqlc.arg('amount');

-- name: UpdateSerialStatus :exec
UPDATE inventory.sku_serial
SET status = sqlc.arg('status')
WHERE id = ANY(sqlc.slice('id')) OR serial_number = ANY(sqlc.slice('serial_number'));