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
INNER JOIN "order"."order" o ON o.transport_id = t.id
WHERE t.id = $1;

-- name: GetTransportByTrackingID :one
-- Look up transport by provider tracking ID stored in JSONB data field.
SELECT * FROM "order"."transport" WHERE "data"->>'tracking_id' = sqlc.arg(tracking_id)::text LIMIT 1;
