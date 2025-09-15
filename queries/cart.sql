-- name: RemoveCheckoutItem :many
DELETE FROM "account"."cart_item"
WHERE cart_id = sqlc.arg('cart_id')
AND sku_id = ANY(sqlc.slice('sku_id'))
RETURNING *;

-- name: UpdateCart :exec
WITH updated AS (
    UPDATE "account"."cart_item"
    SET quantity = $3
    WHERE cart_id = $1 AND sku_id = $2
    RETURNING 1
)
INSERT INTO "account"."cart_item" (cart_id, sku_id, quantity)
SELECT $1, $2, $3
WHERE NOT EXISTS (SELECT 1 FROM updated);