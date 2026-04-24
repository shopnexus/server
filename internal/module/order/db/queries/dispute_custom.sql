-- =============================================
-- Module:      order
-- File:        dispute_custom.sql
-- Purpose:     Refund dispute escalation (either party can raise, platform resolves).
-- =============================================

-- name: CreateRefundDispute :one
INSERT INTO "order"."refund_dispute" (
    "account_id", "refund_id", "reason", "note"
) VALUES (
    @account_id, @refund_id, @reason, @note
)
RETURNING *;

-- name: GetRefundDispute :one
SELECT * FROM "order"."refund_dispute" WHERE "id" = @id;

-- name: ResolveRefundDispute :one
UPDATE "order"."refund_dispute"
SET "status" = @status,
    "resolved_by_id" = @resolved_by_id,
    "note" = @note,
    "date_resolved" = CURRENT_TIMESTAMP
WHERE "id" = @id AND "status" = 'Pending'
RETURNING *;

-- name: ListRefundDisputes :many
SELECT * FROM "order"."refund_dispute"
WHERE "status" = @status
ORDER BY "date_created" DESC
LIMIT @limit_count::INTEGER OFFSET @offset_count::INTEGER;

-- name: ListRefundDisputesByRefund :many
SELECT * FROM "order"."refund_dispute"
WHERE "refund_id" = @refund_id
ORDER BY "date_created" DESC;
