CREATE SCHEMA IF NOT EXISTS "inventory";

CREATE TYPE "inventory"."stock_ref_type" AS ENUM ('ProductSku', 'Promotion');
CREATE TYPE "inventory"."status" AS ENUM ('Active', 'Inactive', 'Taken', 'Damaged');

CREATE TABLE IF NOT EXISTS "inventory"."serial" (
    "id" TEXT NOT NULL,
    "ref_type" "inventory"."stock_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "status" "inventory"."status" NOT NULL DEFAULT 'Active',
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "serial_pkey" PRIMARY KEY ("id")
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

CREATE INDEX IF NOT EXISTS "serial_ref_id_ref_type_idx" ON "inventory"."serial" ("ref_id", "ref_type");
CREATE UNIQUE INDEX IF NOT EXISTS "stock_ref_id_ref_type_key" ON "inventory"."stock" ("ref_id", "ref_type");
CREATE INDEX IF NOT EXISTS "stock_history_stock_id_idx" ON "inventory"."stock_history" ("stock_id");
CREATE INDEX IF NOT EXISTS "stock_history_date_created_idx" ON "inventory"."stock_history" ("date_created");

ALTER TABLE "inventory"."stock_history"
    ADD CONSTRAINT "stock_history_stock_id_fkey"
    FOREIGN KEY ("stock_id") REFERENCES "inventory"."stock" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

