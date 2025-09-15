-- name: GetAvailableProducts :batchmany
SELECT id, sku_id, serial_number
FROM "inventory"."sku_serial"
WHERE sku_id = sqlc.arg('sku_id') AND "status" = 'Active'
ORDER BY date_created DESC LIMIT sqlc.arg('amount');

-- name: ReserveInventory :batchexec
UPDATE inventory.stock
SET current_stock = current_stock - sqlc.arg('amount'),
    sold = sold + sqlc.arg('amount')
WHERE ref_type = sqlc.arg('ref_type')
  AND ref_id = sqlc.arg('ref_id')
  AND current_stock >= sqlc.arg('amount');