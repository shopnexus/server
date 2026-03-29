-- Drops all catalog schema objects in reverse dependency order.
-- Foreign key constraints are removed before indexes and tables.

-- Foreign key constraints (join/child tables first)
ALTER TABLE IF EXISTS "catalog"."product_spu_tag"
    DROP CONSTRAINT IF EXISTS "product_spu_tag_tag_fkey";

ALTER TABLE IF EXISTS "catalog"."product_spu_tag"
    DROP CONSTRAINT IF EXISTS "product_spu_tag_spu_id_fkey";

ALTER TABLE IF EXISTS "catalog"."product_sku"
    DROP CONSTRAINT IF EXISTS "product_sku_spu_id_fkey";

ALTER TABLE IF EXISTS "catalog"."product_spu"
    DROP CONSTRAINT IF EXISTS "product_spu_category_id_fkey";

-- Indexes
DROP INDEX IF EXISTS "search_sync_ref_type_ref_id_key";
DROP INDEX IF EXISTS "search_sync_date_created_idx";
DROP INDEX IF EXISTS "search_sync_is_stale_metadata_idx";
DROP INDEX IF EXISTS "search_sync_is_stale_embedding_idx";
DROP INDEX IF EXISTS "search_sync_ref_type_ref_id_idx";
DROP INDEX IF EXISTS "product_spu_tag_spu_id_tag_key";
DROP INDEX IF EXISTS "product_sku_spu_id_idx";
DROP INDEX IF EXISTS "product_spu_category_id_idx";
DROP INDEX IF EXISTS "product_spu_account_id_idx";
DROP INDEX IF EXISTS "product_spu_featured_sku_id_key";
DROP INDEX IF EXISTS "product_spu_slug_key";
DROP INDEX IF EXISTS "category_parent_id_idx";
DROP INDEX IF EXISTS "category_name_key";
-- Tables (dependent tables first)
DROP TABLE IF EXISTS "catalog"."search_sync";
DROP TABLE IF EXISTS "catalog"."comment";
DROP TABLE IF EXISTS "catalog"."product_spu_tag";
DROP TABLE IF EXISTS "catalog"."tag";
DROP TABLE IF EXISTS "catalog"."product_sku";
DROP TABLE IF EXISTS "catalog"."product_spu";
DROP TABLE IF EXISTS "catalog"."category";
-- Enums
DROP TYPE IF EXISTS "catalog"."comment_ref_type";
DROP TYPE IF EXISTS "catalog"."search_sync_ref_type";

DROP SCHEMA IF EXISTS "catalog";
