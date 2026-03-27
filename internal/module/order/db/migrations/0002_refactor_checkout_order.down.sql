-- Reverse of 18: Recreate shipment table
CREATE TABLE IF NOT EXISTS "order"."shipment" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "option" TEXT NOT NULL,
    "tracking_code" TEXT,
    "status" "order"."transport_status" NOT NULL DEFAULT 'Pending',
    "label_url" TEXT,
    "cost" BIGINT NOT NULL,
    "new_cost" BIGINT NOT NULL,
    "date_eta" TIMESTAMPTZ(3) NOT NULL,
    "from_address" TEXT NOT NULL,
    "to_address" TEXT NOT NULL,
    "weight_grams" INTEGER NOT NULL,
    "length_cm" INTEGER NOT NULL,
    "width_cm" INTEGER NOT NULL,
    "height_cm" INTEGER NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "shipment_pkey" PRIMARY KEY ("id")
);

-- Reverse of 17: Restore shipment data from transport
INSERT INTO "order"."shipment" (
    "id", "option", "tracking_code", "status", "label_url",
    "cost", "new_cost", "date_eta", "from_address", "to_address",
    "weight_grams", "length_cm", "width_cm", "height_cm", "date_created"
)
SELECT
    "id",
    "option",
    ("data"->>'tracking_code')::TEXT,
    "status"::text::"order"."transport_status",
    ("data"->>'label_url')::TEXT,
    "cost",
    COALESCE(("data"->>'new_cost')::BIGINT, 0),
    COALESCE(("data"->>'date_eta')::TIMESTAMPTZ, CURRENT_TIMESTAMP),
    COALESCE(("data"->>'from_address')::TEXT, ''),
    COALESCE(("data"->>'to_address')::TEXT, ''),
    COALESCE(("data"->>'weight_grams')::INTEGER, 0),
    COALESCE(("data"->>'length_cm')::INTEGER, 0),
    COALESCE(("data"->>'width_cm')::INTEGER, 0),
    COALESCE(("data"->>'height_cm')::INTEGER, 0),
    "date_created"
FROM "order"."transport";

-- Reverse of 16: Restore payment FK to NO ACTION
ALTER TABLE "order"."order" DROP CONSTRAINT IF EXISTS "order_payment_id_fkey";
ALTER TABLE "order"."order"
    ADD CONSTRAINT "order_payment_id_fkey"
    FOREIGN KEY ("payment_id") REFERENCES "order"."payment" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- Reverse of 15: Restore indexes
DROP INDEX IF EXISTS "order_seller_id_idx";
CREATE INDEX IF NOT EXISTS "order_vendor_id_idx" ON "order"."order" ("seller_id");

DROP INDEX IF EXISTS "refund_transport_id_idx";
CREATE INDEX IF NOT EXISTS "refund_shipment_id_idx" ON "order"."refund" ("transport_id");

-- Reverse of 14: Restore refund shipment FK
ALTER TABLE "order"."refund" DROP CONSTRAINT IF EXISTS "refund_transport_id_fkey";
ALTER TABLE "order"."refund" RENAME COLUMN "transport_id" TO "shipment_id";
ALTER TABLE "order"."refund"
    ADD CONSTRAINT "refund_shipment_id_fkey"
    FOREIGN KEY ("shipment_id") REFERENCES "order"."shipment" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- Reverse of 13: Restore order shipment FK
ALTER TABLE "order"."order" DROP CONSTRAINT IF EXISTS "order_transport_id_fkey";
ALTER TABLE "order"."order" RENAME COLUMN "transport_id" TO "shipment_id";
ALTER TABLE "order"."order"
    ADD CONSTRAINT "order_shipment_id_fkey"
    FOREIGN KEY ("shipment_id") REFERENCES "order"."shipment" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- Reverse of 12: Restore payment_id NOT NULL
ALTER TABLE "order"."order"
    ALTER COLUMN "payment_id" SET NOT NULL;

-- Reverse of 11: Restore ship_discount column
ALTER TABLE "order"."order"
    ADD COLUMN IF NOT EXISTS "ship_discount" BIGINT NOT NULL DEFAULT 0;

-- Reverse of 10: Rename columns back
ALTER TABLE "order"."order" RENAME COLUMN "buyer_id" TO "customer_id";
ALTER TABLE "order"."order" RENAME COLUMN "seller_id" TO "vendor_id";
ALTER TABLE "order"."order" RENAME COLUMN "transport_cost" TO "ship_cost";

-- Reverse of 9: Drop partial indexes
DROP INDEX IF EXISTS "item_pending_seller_idx";
DROP INDEX IF EXISTS "item_pending_account_idx";

-- Reverse of 8: Restore UNIQUE(order_id, sku_id) constraint
CREATE UNIQUE INDEX IF NOT EXISTS "item_order_id_sku_id_key" ON "order"."item" ("order_id", "sku_id");

-- Reverse of 7: Make order_id NOT NULL again
ALTER TABLE "order"."item"
    ALTER COLUMN "order_id" SET NOT NULL;

-- Reverse of 6+4: Drop added item columns
ALTER TABLE "order"."item"
    DROP COLUMN IF EXISTS "account_id",
    DROP COLUMN IF EXISTS "seller_id",
    DROP COLUMN IF EXISTS "address",
    DROP COLUMN IF EXISTS "status",
    DROP COLUMN IF EXISTS "paid_amount",
    DROP COLUMN IF EXISTS "date_created",
    DROP COLUMN IF EXISTS "date_updated";

-- Reverse of 3: Drop transport table
DROP TABLE IF EXISTS "order"."transport";

-- Reverse of 2: Rename transport_status back to shipment_status
ALTER TYPE "order"."transport_status" RENAME TO "shipment_status";

-- Reverse of 1: Drop item_status enum
DROP TYPE IF EXISTS "order"."item_status";
