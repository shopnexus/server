-- =============================================
-- Migration 0002: Dispute resolution + Partial refund
-- =============================================

-- ---------------------------------------------
-- Feature 1: Dispute resolution metadata
-- ---------------------------------------------

ALTER TABLE "order"."refund_dispute"
    ADD COLUMN IF NOT EXISTS "resolved_by_id" UUID,
    ADD COLUMN IF NOT EXISTS "resolution_note" TEXT,
    ADD COLUMN IF NOT EXISTS "date_resolved" TIMESTAMPTZ(3);

COMMENT ON COLUMN "order"."refund_dispute"."resolved_by_id" IS 'Account that resolved the dispute (platform staff with elevated permissions)';
COMMENT ON COLUMN "order"."refund_dispute"."resolution_note" IS 'Free-form note explaining the resolution decision';
COMMENT ON COLUMN "order"."refund_dispute"."date_resolved" IS 'When resolution was recorded';

CREATE INDEX IF NOT EXISTS "refund_dispute_resolved_by_id_idx" ON "order"."refund_dispute" ("resolved_by_id");

-- ---------------------------------------------
-- Feature 7: Partial refund
-- item_ids: NULL = refund all items of order; array = specific items being refunded
-- amount: NULL = full order total; value = custom partial amount (must match items aggregate)
-- ---------------------------------------------

ALTER TABLE "order"."refund"
    ADD COLUMN IF NOT EXISTS "item_ids" JSONB,
    ADD COLUMN IF NOT EXISTS "amount" BIGINT;

COMMENT ON COLUMN "order"."refund"."item_ids" IS 'Array of item IDs being refunded; NULL means refund all items of the order (full refund)';
COMMENT ON COLUMN "order"."refund"."amount" IS 'Partial refund amount in smallest currency unit; NULL means use order total (full refund)';
