-- =============================================
-- Module: Inventory
-- Schema: inventory
-- Description: Stock level tracking, per-unit serial number management,
--              and an audit log of all stock changes. Supports both
--              simple quantity-based and serialized (per-unit) inventory.
-- =============================================

CREATE SCHEMA IF NOT EXISTS "inventory";

-- Enums

-- What kind of entity owns this stock record
CREATE TYPE "inventory"."stock_ref_type" AS ENUM ('ProductSku', 'Promotion');
-- Lifecycle state of an individual serialized unit
CREATE TYPE "inventory"."status" AS ENUM ('Active', 'Inactive', 'Taken', 'Damaged');

-- Tables

-- Stock level record for a product SKU or promotion allocation.
-- "stock" is the total quantity on hand; "taken" is how many are reserved/sold.
-- serial_required = true means each unit must have a serial number in the serial table.
CREATE TABLE IF NOT EXISTS "inventory"."stock" (
    "id" BIGSERIAL NOT NULL,
    "ref_type" "inventory"."stock_ref_type" NOT NULL,
    -- ID of the owning SKU or promotion
    "ref_id" UUID NOT NULL,
    -- Total quantity in stock
    "stock" BIGINT NOT NULL,
    -- Quantity currently reserved or sold
    "taken" BIGINT NOT NULL DEFAULT 0,
    -- When true, each unit must be tracked individually in the serial table
    "serial_required" BOOLEAN NOT NULL,
    "date_created" TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "stock_pkey" PRIMARY KEY ("id")
);

-- Per-unit serial number for serialized inventory items (e.g. electronics with IMEI).
-- Only populated when the parent stock.serial_required = true.
CREATE TABLE IF NOT EXISTS "inventory"."serial" (
    -- The serial number / IMEI / license key itself
    "id" TEXT NOT NULL,
    "stock_id" BIGINT NOT NULL,
    "status" "inventory"."status" NOT NULL DEFAULT 'Active',
    "date_created" TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "serial_pkey" PRIMARY KEY ("id")
);

-- Append-only audit log of every stock level change (restocks, sales, adjustments).
-- "change" is a signed delta (positive = added stock, negative = removed).
CREATE TABLE IF NOT EXISTS "inventory"."stock_history" (
    "id" BIGSERIAL NOT NULL,
    "stock_id" BIGINT NOT NULL,
    -- Signed quantity delta (positive = restock, negative = sale/adjustment)
    "change" BIGINT NOT NULL,
    "date_created" TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "stock_history_pkey" PRIMARY KEY ("id")
);

-- Indexes

-- Unique per entity: one stock record per SKU/promotion
CREATE UNIQUE INDEX IF NOT EXISTS "stock_ref_id_ref_type_key" ON "inventory"."stock" ("ref_id", "ref_type");
CREATE INDEX IF NOT EXISTS "serial_stock_id_idx" ON "inventory"."serial" ("stock_id");
CREATE INDEX IF NOT EXISTS "stock_history_stock_id_idx" ON "inventory"."stock_history" ("stock_id");
CREATE INDEX IF NOT EXISTS "stock_history_date_created_idx" ON "inventory"."stock_history" ("date_created");
-- Partial index: quickly find available (unassigned) serial units for a stock record
CREATE INDEX IF NOT EXISTS "serial_active_stock_id_idx"
    ON "inventory"."serial" ("stock_id")
    WHERE "status" = 'Active';

-- Foreign keys

ALTER TABLE "inventory"."stock_history"
    ADD CONSTRAINT "stock_history_stock_id_fkey"
    FOREIGN KEY ("stock_id") REFERENCES "inventory"."stock" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "inventory"."serial"
    ADD CONSTRAINT "serial_stock_id_fkey"
    FOREIGN KEY ("stock_id") REFERENCES "inventory"."stock" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

COMMENT ON TABLE "inventory"."serial" IS 'Per-unit serial number tracking for serialized inventory items';
COMMENT ON TABLE "inventory"."stock" IS 'Tracks available and reserved stock per product SKU or promotion';
COMMENT ON TABLE "inventory"."stock_history" IS 'Append-only audit log of all stock level changes';
