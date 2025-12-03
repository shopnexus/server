DROP INDEX IF EXISTS "interaction_ref_type_ref_id_idx";
DROP INDEX IF EXISTS "interaction_event_type_idx";
DROP INDEX IF EXISTS "interaction_session_id_idx";
DROP INDEX IF EXISTS "interaction_account_id_idx";

DROP TABLE IF EXISTS "analytic"."interaction";

DROP TYPE IF EXISTS "analytic"."interaction_ref_type";

DROP SCHEMA IF EXISTS "analytic";

