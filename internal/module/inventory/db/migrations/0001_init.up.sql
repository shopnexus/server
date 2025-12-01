CREATE SCHEMA IF NOT EXISTS "inventory";

CREATE TYPE "inventory"."stock_ref_type" AS ENUM ('ProductSku', 'Promotion');
CREATE TYPE "inventory"."product_status" AS ENUM ('Active', 'Inactive', 'Sold', 'Damaged');

CREATE TABLE IF NOT EXISTS "inventory"."sku_serial" (
    "id" TEXT NOT NULL,
    "sku_id" UUID NOT NULL,
    "status" "inventory"."product_status" NOT NULL DEFAULT 'Active',
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "sku_serial_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "inventory"."stock" (
    "id" BIGSERIAL NOT NULL,
    "ref_type" "inventory"."stock_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "stock" BIGINT NOT NULL,
    "taken" BIGINT NOT NULL DEFAULT 0,
    "serial_required" BOOLEAN NOT NULL DEFAULT false,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "stock_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "inventory"."stock_history" (
    "id" BIGSERIAL NOT NULL,
    "stock_id" BIGINT NOT NULL,
    "change" BIGINT NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "stock_history_pkey" PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "sku_serial_sku_id_idx" ON "inventory"."sku_serial" ("sku_id");
CREATE UNIQUE INDEX IF NOT EXISTS "stock_ref_id_ref_type_key" ON "inventory"."stock" ("ref_id", "ref_type");
CREATE INDEX IF NOT EXISTS "stock_history_stock_id_idx" ON "inventory"."stock_history" ("stock_id");
CREATE INDEX IF NOT EXISTS "stock_history_date_created_idx" ON "inventory"."stock_history" ("date_created");

ALTER TABLE "inventory"."sku_serial"
    ADD CONSTRAINT "sku_serial_sku_id_fkey"
    FOREIGN KEY ("sku_id") REFERENCES "catalog"."product_sku" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "inventory"."stock_history"
    ADD CONSTRAINT "stock_history_stock_id_fkey"
    FOREIGN KEY ("stock_id") REFERENCES "inventory"."stock" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

