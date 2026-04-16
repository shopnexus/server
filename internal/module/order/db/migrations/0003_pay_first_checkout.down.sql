DROP INDEX IF EXISTS "order".idx_item_seller_pending;
DROP INDEX IF EXISTS "order".idx_item_payment_id;

ALTER TABLE "order"."order" ADD COLUMN payment_id BIGINT;

CREATE TYPE "order".item_status AS ENUM ('Pending', 'Confirmed', 'Cancelled');
ALTER TABLE "order".item ADD COLUMN status "order".item_status NOT NULL DEFAULT 'Pending';

ALTER TABLE "order".item DROP COLUMN IF EXISTS date_cancelled;
ALTER TABLE "order".item DROP COLUMN IF EXISTS payment_id;
ALTER TABLE "order".item DROP COLUMN IF EXISTS transport_cost_estimate;
ALTER TABLE "order".item DROP COLUMN IF EXISTS transport_option;
