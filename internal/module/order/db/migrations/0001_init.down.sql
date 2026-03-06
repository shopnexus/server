ALTER TABLE IF EXISTS "order"."refund_dispute"
    DROP CONSTRAINT IF EXISTS "refund_dispute_issued_by_id_fkey";

ALTER TABLE IF EXISTS "order"."refund_dispute"
    DROP CONSTRAINT IF EXISTS "refund_dispute_refund_id_fkey";

ALTER TABLE IF EXISTS "order"."refund"
    DROP CONSTRAINT IF EXISTS "refund_shipment_id_fkey";

ALTER TABLE IF EXISTS "order"."refund"
    DROP CONSTRAINT IF EXISTS "refund_confirmed_by_id_fkey";

ALTER TABLE IF EXISTS "order"."refund"
    DROP CONSTRAINT IF EXISTS "refund_order_id_fkey";

ALTER TABLE IF EXISTS "order"."refund"
    DROP CONSTRAINT IF EXISTS "refund_account_id_fkey";

ALTER TABLE IF EXISTS "order"."item"
    DROP CONSTRAINT IF EXISTS "item_sku_id_fkey";

ALTER TABLE IF EXISTS "order"."item"
    DROP CONSTRAINT IF EXISTS "item_order_id_fkey";

ALTER TABLE IF EXISTS "order"."order"
    DROP CONSTRAINT IF EXISTS "order_confirmed_by_id_fkey";

ALTER TABLE IF EXISTS "order"."order"
    DROP CONSTRAINT IF EXISTS "order_shipment_id_fkey";

ALTER TABLE IF EXISTS "order"."order"
    DROP CONSTRAINT IF EXISTS "order_payment_id_fkey";

ALTER TABLE IF EXISTS "order"."order"
    DROP CONSTRAINT IF EXISTS "order_vendor_id_fkey";

ALTER TABLE IF EXISTS "order"."order"
    DROP CONSTRAINT IF EXISTS "order_customer_id_fkey";

ALTER TABLE IF EXISTS "order"."shipment"
    DROP CONSTRAINT IF EXISTS "shipment_option_fkey";

ALTER TABLE IF EXISTS "order"."payment"
    DROP CONSTRAINT IF EXISTS "payment_option_fkey";

ALTER TABLE IF EXISTS "order"."payment"
    DROP CONSTRAINT IF EXISTS "payment_account_id_fkey";

ALTER TABLE IF EXISTS "order"."cart_item"
    DROP CONSTRAINT IF EXISTS "cart_item_sku_id_fkey";

ALTER TABLE IF EXISTS "order"."cart_item"
    DROP CONSTRAINT IF EXISTS "cart_item_account_id_fkey";

DROP INDEX IF EXISTS "refund_dispute_issued_by_id_idx";
DROP INDEX IF EXISTS "refund_dispute_refund_id_idx";
DROP INDEX IF EXISTS "refund_shipment_id_idx";
DROP INDEX IF EXISTS "refund_confirmed_by_id_idx";
DROP INDEX IF EXISTS "refund_order_id_idx";
DROP INDEX IF EXISTS "refund_account_id_idx";
DROP INDEX IF EXISTS "item_order_id_sku_id_key";
DROP INDEX IF EXISTS "item_sku_id_idx";
DROP INDEX IF EXISTS "item_order_id_idx";
DROP INDEX IF EXISTS "order_vendor_id_idx";
-- DROP INDEX IF EXISTS "order_payment_id_key";
DROP INDEX IF EXISTS "cart_item_account_id_sku_id_key";

DROP TABLE IF EXISTS "order"."refund_dispute";
DROP TABLE IF EXISTS "order"."refund";
DROP TABLE IF EXISTS "order"."item";
DROP TABLE IF EXISTS "order"."order";
DROP TABLE IF EXISTS "order"."shipment";
DROP TABLE IF EXISTS "order"."payment";
DROP TABLE IF EXISTS "order"."cart_item";

DROP TYPE IF EXISTS "order"."shipment_status";
DROP TYPE IF EXISTS "order"."refund_method";
DROP TYPE IF EXISTS "order"."status";

DROP SCHEMA IF EXISTS "order";

