-- Drops all analytic schema objects in reverse dependency order.

-- Indexes
DROP INDEX IF EXISTS "product_popularity_score_idx";
DROP INDEX IF EXISTS "interaction_ref_type_ref_id_idx";
DROP INDEX IF EXISTS "interaction_event_type_idx";
DROP INDEX IF EXISTS "interaction_session_id_idx";
DROP INDEX IF EXISTS "interaction_account_id_idx";

-- Tables
DROP TABLE IF EXISTS "analytic"."product_popularity";
DROP TABLE IF EXISTS "analytic"."interaction";

-- Enums
DROP TYPE IF EXISTS "analytic"."interaction_ref_type";

DROP SCHEMA IF EXISTS "analytic";
