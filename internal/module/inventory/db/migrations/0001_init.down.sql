ALTER TABLE IF EXISTS "inventory"."stock_history"
    DROP CONSTRAINT IF EXISTS "stock_history_stock_id_fkey";

ALTER TABLE IF EXISTS "inventory"."serial"
    DROP CONSTRAINT IF EXISTS "serial_ref_id_ref_type_fkey";

DROP INDEX IF EXISTS "stock_history_date_created_idx";
DROP INDEX IF EXISTS "stock_history_stock_id_idx";
DROP INDEX IF EXISTS "stock_ref_id_ref_type_key";
DROP INDEX IF EXISTS "serial_ref_id_ref_type_idx";

DROP TABLE IF EXISTS "inventory"."stock_history";
DROP TABLE IF EXISTS "inventory"."stock";
DROP TABLE IF EXISTS "inventory"."serial";

DROP TYPE IF EXISTS "inventory"."status";
DROP TYPE IF EXISTS "inventory"."stock_ref_type";

DROP SCHEMA IF EXISTS "inventory";

