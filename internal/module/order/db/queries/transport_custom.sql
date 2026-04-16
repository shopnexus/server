-- Custom transport queries for webhook-driven status updates.
-- See: https://docs.giaohangtietkiem.vn/webhook

-- name: UpdateTransportStatus :exec
UPDATE "order"."transport"
SET status = $2,
    data   = data || $3::jsonb
WHERE id = $1;

-- name: GetTransportWithOrder :one
SELECT t.*,
       o.id        AS order_id,
       o.buyer_id  AS order_buyer_id,
       o.seller_id AS order_seller_id
FROM "order"."transport" t
LEFT JOIN "order"."order" o ON o.transport_id = t.id
WHERE t.id = $1;
