-- name: RemoveCheckoutItem :many
DELETE FROM "account"."cart_item"
WHERE cart_id = sqlc.arg('cart_id')
AND sku_id = ANY(sqlc.slice('sku_id'))
RETURNING *;