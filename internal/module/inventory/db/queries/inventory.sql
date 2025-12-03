-- name: GetAvailableSerials :many
SELECT id, ref_type, ref_id
FROM "inventory"."serial"
WHERE ref_type = sqlc.arg('ref_type') AND ref_id = sqlc.arg('ref_id') AND "status" = 'Active'
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
UPDATE inventory.serial
SET status = sqlc.arg('status')
WHERE id = ANY(sqlc.slice('id'));

-- name: UpdateCurrentStock :exec
UPDATE "inventory"."stock"
SET "stock" = stock + sqlc.arg('change')
WHERE id = sqlc.arg('id');

-- name: ListMostTaken :many
SELECT se.*
FROM "inventory"."serial" se
INNER JOIN "inventory"."stock" st ON se.ref_id = st.ref_id AND se.ref_type = st.ref_type
WHERE se.ref_type = sqlc.arg('ref_type')
ORDER BY st.taken DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;