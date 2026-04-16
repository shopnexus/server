-- =============================================
-- Rollback for 0002
-- =============================================

DROP INDEX IF EXISTS "order"."refund_dispute_resolved_by_id_idx";

ALTER TABLE "order"."refund" DROP COLUMN IF EXISTS "amount";
ALTER TABLE "order"."refund" DROP COLUMN IF EXISTS "item_ids";

ALTER TABLE "order"."refund_dispute" DROP COLUMN IF EXISTS "date_resolved";
ALTER TABLE "order"."refund_dispute" DROP COLUMN IF EXISTS "resolution_note";
ALTER TABLE "order"."refund_dispute" DROP COLUMN IF EXISTS "resolved_by_id";
