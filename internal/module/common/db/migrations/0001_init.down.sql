-- Drops all common schema objects in reverse dependency order.

-- Foreign key constraints
ALTER TABLE IF EXISTS "common"."resource_reference"
    DROP CONSTRAINT IF EXISTS "resource_reference_rs_id_fkey";

-- Indexes
DROP INDEX IF EXISTS "service_option_category_provider_idx";
DROP INDEX IF EXISTS "resource_provider_object_key_key";

-- Tables (child tables first)
DROP TABLE IF EXISTS "common"."resource_reference";
DROP TABLE IF EXISTS "common"."service_option";
DROP TABLE IF EXISTS "common"."resource";

-- Enums
DROP TYPE IF EXISTS "common"."resource_ref_type";

DROP SCHEMA IF EXISTS "common";
