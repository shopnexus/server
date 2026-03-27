-- 1. Create new enum for item status
CREATE TYPE "order"."item_status" AS ENUM ('Pending', 'Confirmed', 'Canceled');

-- 2. Rename shipment_status -> transport_status
ALTER TYPE "order"."shipment_status" RENAME TO "transport_status";

-- 3. Create transport table
CREATE TABLE IF NOT EXISTS "order"."transport" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "option" TEXT NOT NULL,
    "status" "order"."transport_status" DEFAULT 'Pending',
    "cost" BIGINT NOT NULL DEFAULT 0,
    "data" JSONB NOT NULL DEFAULT '{}',
    "date_created" TIMESTAMPTZ(3) DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "transport_pkey" PRIMARY KEY ("id")
);

-- 4. Add new columns to order.item (nullable first for backfill)
ALTER TABLE "order"."item"
    ADD COLUMN IF NOT EXISTS "account_id" UUID,
    ADD COLUMN IF NOT EXISTS "seller_id" UUID,
    ADD COLUMN IF NOT EXISTS "address" TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS "status" "order"."item_status" NOT NULL DEFAULT 'Pending',
    ADD COLUMN IF NOT EXISTS "paid_amount" BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ADD COLUMN IF NOT EXISTS "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP;

-- 5. Backfill item columns from their parent orders
UPDATE "order"."item" i SET
    "account_id" = o."customer_id",
    "seller_id" = o."vendor_id",
    "address" = o."address",
    "status" = 'Confirmed',
    "paid_amount" = i."unit_price" * i."quantity",
    "date_created" = o."date_created"
FROM "order"."order" o WHERE i."order_id" = o."id";

-- 6. Make account_id and seller_id NOT NULL after backfill
ALTER TABLE "order"."item"
    ALTER COLUMN "account_id" SET NOT NULL,
    ALTER COLUMN "seller_id" SET NOT NULL;

-- 7. Make order_id nullable
ALTER TABLE "order"."item"
    ALTER COLUMN "order_id" DROP NOT NULL;

-- 8. Drop UNIQUE(order_id, sku_id) constraint
DROP INDEX IF EXISTS "order"."item_order_id_sku_id_key";

-- 9. Create partial indexes for pending (cart) items
CREATE INDEX IF NOT EXISTS "item_pending_seller_idx" ON "order"."item" ("seller_id", "status") WHERE "order_id" IS NULL;
CREATE INDEX IF NOT EXISTS "item_pending_account_idx" ON "order"."item" ("account_id", "status") WHERE "order_id" IS NULL;

-- 10. Rename columns on order.order
ALTER TABLE "order"."order" RENAME COLUMN "customer_id" TO "buyer_id";
ALTER TABLE "order"."order" RENAME COLUMN "vendor_id" TO "seller_id";
ALTER TABLE "order"."order" RENAME COLUMN "ship_cost" TO "transport_cost";

-- 11. Drop ship_discount column
ALTER TABLE "order"."order" DROP COLUMN IF EXISTS "ship_discount";

-- 12. Make payment_id nullable
ALTER TABLE "order"."order"
    ALTER COLUMN "payment_id" DROP NOT NULL;

-- 13. Handle shipment -> transport FK on order.order
ALTER TABLE "order"."order" DROP CONSTRAINT IF EXISTS "order_shipment_id_fkey";
ALTER TABLE "order"."order" RENAME COLUMN "shipment_id" TO "transport_id";
ALTER TABLE "order"."order"
    ADD CONSTRAINT "order_transport_id_fkey"
    FOREIGN KEY ("transport_id") REFERENCES "order"."transport" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- 14. Handle shipment -> transport FK on order.refund
ALTER TABLE "order"."refund" DROP CONSTRAINT IF EXISTS "refund_shipment_id_fkey";
ALTER TABLE "order"."refund" RENAME COLUMN "shipment_id" TO "transport_id";
ALTER TABLE "order"."refund"
    ADD CONSTRAINT "refund_transport_id_fkey"
    FOREIGN KEY ("transport_id") REFERENCES "order"."transport" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- 15. Rename indexes
DROP INDEX IF EXISTS "order"."order_vendor_id_idx";
CREATE INDEX IF NOT EXISTS "order_seller_id_idx" ON "order"."order" ("seller_id");

DROP INDEX IF EXISTS "order"."refund_shipment_id_idx";
CREATE INDEX IF NOT EXISTS "refund_transport_id_idx" ON "order"."refund" ("transport_id");

-- 16. Update payment FK to allow SET NULL
ALTER TABLE "order"."order" DROP CONSTRAINT IF EXISTS "order_payment_id_fkey";
ALTER TABLE "order"."order"
    ADD CONSTRAINT "order_payment_id_fkey"
    FOREIGN KEY ("payment_id") REFERENCES "order"."payment" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- 17. Migrate shipment data to transport
INSERT INTO "order"."transport" ("id", "option", "status", "cost", "data", "date_created")
SELECT
    "id",
    "option",
    "status"::text::"order"."transport_status",
    "cost",
    jsonb_build_object(
        'tracking_code', "tracking_code",
        'label_url', "label_url",
        'new_cost', "new_cost",
        'date_eta', "date_eta",
        'from_address', "from_address",
        'to_address', "to_address",
        'weight_grams', "weight_grams",
        'length_cm', "length_cm",
        'width_cm', "width_cm",
        'height_cm', "height_cm"
    ),
    "date_created"
FROM "order"."shipment";

-- 18. Drop shipment table
DROP TABLE IF EXISTS "order"."shipment";
