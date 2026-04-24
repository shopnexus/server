-- Custom transport queries for webhook-driven status updates.
-- See: https://docs.giaohangtietkiem.vn/webhook

-- name: CreateTransport :one
INSERT INTO "order"."transport" ("option", "status", "data")
VALUES (@option, @status, @data)
RETURNING *;

-- name: GetTransportByID :one
SELECT * FROM "order"."transport" WHERE "id" = @id;

-- name: UpdateTransportStatusByID :one
UPDATE "order"."transport"
SET "status" = @status, "data" = @data
WHERE "id" = @id
RETURNING *;

-- name: GetTransportByTrackingID :one
SELECT * FROM "order"."transport"
WHERE "data"->>'tracking_id' = @tracking_id
LIMIT 1;

-- name: GetTransportWithOrder :one
SELECT t.*,
       o.id        AS order_id,
       o.buyer_id  AS order_buyer_id,
       o.seller_id AS order_seller_id
FROM "order"."transport" t
INNER JOIN "order"."order" o ON o.transport_id = t.id
WHERE t.id = @id;
