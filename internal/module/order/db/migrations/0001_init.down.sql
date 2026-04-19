-- Drops all order schema objects in reverse dependency order.

-- Foreign key constraints (most-dependent first)
ALTER TABLE IF EXISTS "order"."refund_dispute"
    DROP CONSTRAINT IF EXISTS "refund_dispute_refund_id_fkey";

ALTER TABLE IF EXISTS "order"."refund"
    DROP CONSTRAINT IF EXISTS "refund_transport_id_fkey";

ALTER TABLE IF EXISTS "order"."refund"
    DROP CONSTRAINT IF EXISTS "refund_order_id_fkey";

ALTER TABLE IF EXISTS "order"."item"
    DROP CONSTRAINT IF EXISTS "item_payment_id_fkey";

ALTER TABLE IF EXISTS "order"."item"
    DROP CONSTRAINT IF EXISTS "item_order_id_fkey";

ALTER TABLE IF EXISTS "order"."order"
    DROP CONSTRAINT IF EXISTS "order_transport_id_fkey";

-- Indexes
DROP INDEX IF EXISTS "order"."refund_dispute_resolved_by_id_idx";
DROP INDEX IF EXISTS "order"."refund_dispute_issued_by_id_idx";
DROP INDEX IF EXISTS "order"."refund_dispute_refund_id_idx";
DROP INDEX IF EXISTS "order"."refund_transport_id_idx";
DROP INDEX IF EXISTS "order"."refund_confirmed_by_id_idx";
DROP INDEX IF EXISTS "order"."refund_order_id_idx";
DROP INDEX IF EXISTS "order"."refund_account_id_idx";
DROP INDEX IF EXISTS "order"."idx_item_seller_pending";
DROP INDEX IF EXISTS "order"."idx_item_payment_id";
DROP INDEX IF EXISTS "order"."item_sku_id_idx";
DROP INDEX IF EXISTS "order"."item_order_id_idx";
DROP INDEX IF EXISTS "order"."order_seller_id_idx";
DROP INDEX IF EXISTS "order"."cart_item_account_id_sku_id_key";

-- Tables (most-dependent first)
DROP TABLE IF EXISTS "order"."refund_dispute";
DROP TABLE IF EXISTS "order"."refund";
DROP TABLE IF EXISTS "order"."item";
DROP TABLE IF EXISTS "order"."order";
DROP TABLE IF EXISTS "order"."transport";
DROP TABLE IF EXISTS "order"."payment";
DROP TABLE IF EXISTS "order"."cart_item";

-- Enums
DROP TYPE IF EXISTS "order"."transport_status";
DROP TYPE IF EXISTS "order"."refund_method";
DROP TYPE IF EXISTS "order"."status";

DROP SCHEMA IF EXISTS "order";
