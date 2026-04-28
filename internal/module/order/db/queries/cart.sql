-- name: RemoveCheckoutItem :many
DELETE FROM "order"."cart_item"
WHERE account_id = sqlc.arg('account_id')
AND sku_id = ANY(sqlc.slice('sku_id'))
RETURNING *;

-- name: RestoreCheckoutItems :exec
INSERT INTO "order"."cart_item" (account_id, sku_id, quantity)
SELECT
    UNNEST(sqlc.arg('account_ids')::uuid[]),
    UNNEST(sqlc.arg('sku_ids')::uuid[]),
    UNNEST(sqlc.arg('quantities')::bigint[])
ON CONFLICT (account_id, sku_id) DO UPDATE
    SET quantity = EXCLUDED.quantity;

-- name: UpdateCart :exec
WITH updated AS (
    UPDATE "order"."cart_item"
    SET quantity = $3
    WHERE account_id = $1 AND sku_id = $2
    RETURNING 1
)
INSERT INTO "order"."cart_item" (account_id, sku_id, quantity)
SELECT $1, $2, $3
WHERE NOT EXISTS (SELECT 1 FROM updated);
