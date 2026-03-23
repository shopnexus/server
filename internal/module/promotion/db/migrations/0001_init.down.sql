ALTER TABLE IF EXISTS "promotion"."schedule"
    DROP CONSTRAINT IF EXISTS "schedule_promotion_id_fkey";

ALTER TABLE IF EXISTS "promotion"."ref"
    DROP CONSTRAINT IF EXISTS "ref_promotion_id_fkey";

DROP INDEX IF EXISTS "promotion"."schedule_next_run_idx";
DROP INDEX IF EXISTS "promotion"."ref_ref_type_ref_id_idx";
DROP INDEX IF EXISTS "promotion"."ref_promotion_id_ref_type_ref_id_key";
DROP INDEX IF EXISTS "promotion"."promotion_owner_id_idx";
DROP INDEX IF EXISTS "promotion"."promotion_active_date_idx";
DROP INDEX IF EXISTS "promotion"."promotion_code_key";

DROP TABLE IF EXISTS "promotion"."schedule";
DROP TABLE IF EXISTS "promotion"."ref";
DROP TABLE IF EXISTS "promotion"."promotion";

DROP TYPE IF EXISTS "promotion"."ref_type";
DROP TYPE IF EXISTS "promotion"."type";

DROP SCHEMA IF EXISTS "promotion";
