CREATE SCHEMA IF NOT EXISTS "order";

CREATE TYPE "order"."refund_method" AS ENUM ('PickUp', 'DropOff');
CREATE TYPE "order"."shipment_status" AS ENUM ('Pending', 'LabelCreated', 'InTransit', 'OutForDelivery', 'Delivered', 'Failed', 'Cancelled');
CREATE TYPE "order"."status" AS ENUM ('Pending', 'Processing', 'Success', 'Canceled', 'Failed');

CREATE TABLE IF NOT EXISTS "order"."cart_item" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "sku_id" UUID NOT NULL,
    "quantity" BIGINT NOT NULL,
    CONSTRAINT "cart_item_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "order"."payment" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "option" TEXT NOT NULL,
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
    "amount" BIGINT NOT NULL,
    "data" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_paid" TIMESTAMPTZ(3),
    "date_expired" TIMESTAMPTZ(3) NOT NULL,
    CONSTRAINT "payment_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "order"."shipment" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "option" TEXT NOT NULL,
    "tracking_code" TEXT,
    "status" "order"."shipment_status" NOT NULL DEFAULT 'Pending',
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

CREATE TABLE IF NOT EXISTS "order"."order" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "customer_id" UUID NOT NULL,
    "vendor_id" UUID NOT NULL,
    "payment_id" BIGINT NOT NULL,
    "shipment_id" UUID NOT NULL,
    "confirmed_by_id" UUID,
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
    "address" TEXT NOT NULL,
    "product_cost" BIGINT NOT NULL,
    "product_discount" BIGINT NOT NULL,
    "ship_cost" BIGINT NOT NULL,
    "ship_discount" BIGINT NOT NULL,
    "total" BIGINT NOT NULL,
    "note" TEXT,
    "data" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "order_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "order"."item" (
    "id" BIGSERIAL NOT NULL,
    "order_id" UUID NOT NULL,
    "sku_id" UUID NOT NULL,
    "sku_name" TEXT NOT NULL,
    "quantity" BIGINT NOT NULL,
    "unit_price" BIGINT NOT NULL,
    "note" TEXT,
    "serial_ids" JSONB,
    CONSTRAINT "item_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "order"."refund" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "order_id" UUID NOT NULL,
    "confirmed_by_id" UUID,
    "shipment_id" UUID,
    "method" "order"."refund_method" NOT NULL,
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
    "reason" TEXT NOT NULL,
    "address" TEXT,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "refund_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "order"."refund_dispute" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "refund_id" UUID NOT NULL,
    "issued_by_id" UUID NOT NULL,
    "reason" TEXT NOT NULL,
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "refund_dispute_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "cart_item_account_id_sku_id_key" ON "order"."cart_item" ("account_id", "sku_id");
-- One payment for many orders
-- CREATE UNIQUE INDEX IF NOT EXISTS "order_payment_id_key" ON "order"."order" ("payment_id");
CREATE INDEX IF NOT EXISTS "order_vendor_id_idx" ON "order"."order" ("vendor_id");
CREATE INDEX IF NOT EXISTS "item_order_id_idx" ON "order"."item" ("order_id");
CREATE INDEX IF NOT EXISTS "item_sku_id_idx" ON "order"."item" ("sku_id");
CREATE UNIQUE INDEX IF NOT EXISTS "item_order_id_sku_id_key" ON "order"."item" ("order_id", "sku_id");
CREATE INDEX IF NOT EXISTS "refund_account_id_idx" ON "order"."refund" ("account_id");
CREATE INDEX IF NOT EXISTS "refund_order_id_idx" ON "order"."refund" ("order_id");
CREATE INDEX IF NOT EXISTS "refund_confirmed_by_id_idx" ON "order"."refund" ("confirmed_by_id");
CREATE INDEX IF NOT EXISTS "refund_shipment_id_idx" ON "order"."refund" ("shipment_id");
CREATE INDEX IF NOT EXISTS "refund_dispute_refund_id_idx" ON "order"."refund_dispute" ("refund_id");
CREATE INDEX IF NOT EXISTS "refund_dispute_issued_by_id_idx" ON "order"."refund_dispute" ("issued_by_id");


ALTER TABLE "order"."order"
    ADD CONSTRAINT "order_payment_id_fkey"
    FOREIGN KEY ("payment_id") REFERENCES "order"."payment" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;

ALTER TABLE "order"."order"
    ADD CONSTRAINT "order_shipment_id_fkey"
    FOREIGN KEY ("shipment_id") REFERENCES "order"."shipment" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

ALTER TABLE "order"."item"
    ADD CONSTRAINT "item_order_id_fkey"
    FOREIGN KEY ("order_id") REFERENCES "order"."order" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "order"."refund"
    ADD CONSTRAINT "refund_order_id_fkey"
    FOREIGN KEY ("order_id") REFERENCES "order"."order" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;

ALTER TABLE "order"."refund"
    ADD CONSTRAINT "refund_shipment_id_fkey"
    FOREIGN KEY ("shipment_id") REFERENCES "order"."shipment" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;

ALTER TABLE "order"."refund_dispute"
    ADD CONSTRAINT "refund_dispute_refund_id_fkey"
    FOREIGN KEY ("refund_id") REFERENCES "order"."refund" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;