CREATE SCHEMA IF NOT EXISTS "inventory";

CREATE TYPE "inventory"."stock_ref_type" AS ENUM ('ProductSku', 'Promotion');
CREATE TYPE "inventory"."status" AS ENUM ('Active', 'Inactive', 'Taken', 'Damaged');

CREATE TABLE IF NOT EXISTS "inventory"."stock" (
    "id" BIGSERIAL NOT NULL,
    "ref_type" "inventory"."stock_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "stock" BIGINT NOT NULL,
    "taken" BIGINT NOT NULL DEFAULT 0,
    "serial_required" BOOLEAN NOT NULL DEFAULT false,
    "date_created" TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "stock_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "inventory"."serial" (
    "id" TEXT NOT NULL,
    "stock_id" BIGINT NOT NULL,
    "status" "inventory"."status" NOT NULL DEFAULT 'Active',
    "date_created" TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "serial_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "inventory"."stock_history" (
    "id" BIGSERIAL NOT NULL,
    "stock_id" BIGINT NOT NULL,
    "change" BIGINT NOT NULL,
    "date_created" TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "stock_history_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "stock_ref_id_ref_type_key" ON "inventory"."stock" ("ref_id", "ref_type");
CREATE INDEX IF NOT EXISTS "serial_stock_id_idx" ON "inventory"."serial" ("stock_id");
CREATE INDEX IF NOT EXISTS "stock_history_stock_id_idx" ON "inventory"."stock_history" ("stock_id");
CREATE INDEX IF NOT EXISTS "stock_history_date_created_idx" ON "inventory"."stock_history" ("date_created");

CREATE INDEX IF NOT EXISTS "serial_active_stock_id_idx"
    ON "inventory"."serial" ("stock_id")
    WHERE "status" = 'Active';

ALTER TABLE "inventory"."stock_history"
    ADD CONSTRAINT "stock_history_stock_id_fkey"
    FOREIGN KEY ("stock_id") REFERENCES "inventory"."stock" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "inventory"."serial"
    ADD CONSTRAINT "serial_stock_id_fkey"
    FOREIGN KEY ("stock_id") REFERENCES "inventory"."stock" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

COMMENT ON TABLE "inventory"."serial" IS 'Per-unit serial number tracking for serialized inventory items';
COMMENT ON TABLE "inventory"."stock" IS 'Tracks available and reserved stock per product SKU or promotion';
COMMENT ON TABLE "inventory"."stock_history" IS 'Append-only audit log of all stock level changes';
