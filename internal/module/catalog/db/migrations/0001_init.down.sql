ALTER TABLE IF EXISTS "catalog"."comment"
    DROP CONSTRAINT IF EXISTS "comment_account_id_fkey";

ALTER TABLE IF EXISTS "catalog"."product_spu_tag"
    DROP CONSTRAINT IF EXISTS "product_spu_tag_tag_fkey";

ALTER TABLE IF EXISTS "catalog"."product_spu_tag"
    DROP CONSTRAINT IF EXISTS "product_spu_tag_spu_id_fkey";

ALTER TABLE IF EXISTS "catalog"."product_sku"
    DROP CONSTRAINT IF EXISTS "product_sku_spu_id_fkey";

ALTER TABLE IF EXISTS "catalog"."product_spu"
    DROP CONSTRAINT IF EXISTS "product_spu_brand_id_fkey";

ALTER TABLE IF EXISTS "catalog"."product_spu"
    DROP CONSTRAINT IF EXISTS "product_spu_category_id_fkey";

ALTER TABLE IF EXISTS "catalog"."product_spu"
    DROP CONSTRAINT IF EXISTS "product_spu_account_id_fkey";

DROP INDEX IF EXISTS "search_sync_ref_type_ref_id_key";
DROP INDEX IF EXISTS "search_sync_date_created_idx";
DROP INDEX IF EXISTS "search_sync_is_stale_metadata_idx";
DROP INDEX IF EXISTS "search_sync_is_stale_embedding_idx";
DROP INDEX IF EXISTS "search_sync_ref_type_ref_id_idx";
DROP INDEX IF EXISTS "product_spu_tag_spu_id_tag_key";
DROP INDEX IF EXISTS "product_sku_spu_id_idx";
DROP INDEX IF EXISTS "product_spu_brand_id_idx";
DROP INDEX IF EXISTS "product_spu_category_id_idx";
DROP INDEX IF EXISTS "product_spu_account_id_idx";
DROP INDEX IF EXISTS "product_spu_featured_sku_id_key";
DROP INDEX IF EXISTS "product_spu_slug_key";
DROP INDEX IF EXISTS "category_parent_id_idx";
DROP INDEX IF EXISTS "category_name_key";
DROP INDEX IF EXISTS "brand_code_key";

DROP TABLE IF EXISTS "catalog"."search_sync";
DROP TABLE IF EXISTS "catalog"."comment";
DROP TABLE IF EXISTS "catalog"."product_spu_tag";
DROP TABLE IF EXISTS "catalog"."tag";
DROP TABLE IF EXISTS "catalog"."product_sku";
DROP TABLE IF EXISTS "catalog"."product_spu";
DROP TABLE IF EXISTS "catalog"."category";
DROP TABLE IF EXISTS "catalog"."brand";

DROP TYPE IF EXISTS "catalog"."comment_ref_type";

DROP SCHEMA IF EXISTS "catalog";

