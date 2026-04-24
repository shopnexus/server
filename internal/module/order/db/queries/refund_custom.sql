-- =============================================
-- Module:      order
-- File:        refund_custom.sql
-- Purpose:     2-stage refund workflow (accept → approve) with rejection/dispute hooks.
-- =============================================

-- name: CreateRefund :one
INSERT INTO "order"."refund" (
    "account_id", "order_item_id", "transport_id",
    "method", "reason", "address"
) VALUES (
    @account_id, @order_item_id, @transport_id,
    @method, @reason, @address
)
RETURNING *;

-- name: GetRefund :one
SELECT * FROM "order"."refund" WHERE "id" = @id;

-- name: AcceptRefundStage1 :one
UPDATE "order"."refund"
SET "status" = 'Processing',
    "accepted_by_id" = @accepted_by_id,
    "date_accepted" = CURRENT_TIMESTAMP
WHERE "id" = @id AND "status" = 'Pending'
RETURNING *;

-- name: ApproveRefundStage2 :one
UPDATE "order"."refund"
SET "status" = 'Success',
    "approved_by_id" = @approved_by_id,
    "date_approved" = CURRENT_TIMESTAMP,
    "refund_tx_id" = @refund_tx_id
WHERE "id" = @id AND "status" = 'Processing'
RETURNING *;

-- name: RejectRefund :one
UPDATE "order"."refund"
SET "status" = 'Failed',
    "rejection_note" = @rejection_note
WHERE "id" = @id AND "status" IN ('Pending', 'Processing')
RETURNING *;

-- name: HasActiveRefundForItem :one
SELECT EXISTS (
    SELECT 1 FROM "order"."refund"
    WHERE "order_item_id" = @order_item_id
      AND "status" IN ('Pending', 'Processing')
) AS has_active;

-- name: HasActiveRefundForOrder :one
SELECT EXISTS (
    SELECT 1 FROM "order"."refund" r
    JOIN "order"."item" i ON i."id" = r."order_item_id"
    WHERE i."order_id" = @order_id
      AND r."status" IN ('Pending', 'Processing')
) AS has_active;

-- name: ListBuyerRefunds :many
SELECT * FROM "order"."refund"
WHERE "account_id" = @account_id
ORDER BY "date_created" DESC
LIMIT @limit_count::INTEGER OFFSET @offset_count::INTEGER;
