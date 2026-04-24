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
CREATE TABLE IF NOT EXISTS "inventory"."stock" (
    "id" BIGSERIAL NOT NULL,
    "ref_type" "inventory"."stock_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL, -- ID of the owning SKU or promotion
    "stock" BIGINT NOT NULL, -- Total quantity in stock
    "taken" BIGINT NOT NULL DEFAULT 0, -- Quantity currently reserved or sold
    "serial_required" BOOLEAN NOT NULL, -- When true, each unit must be tracked individually in the serial table
    "date_created" TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "stock_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "stock_ref_id_ref_type_key" UNIQUE ("ref_id", "ref_type")
);

-- Per-unit serial number for serialized inventory items (e.g. electronics with IMEI).
CREATE TABLE IF NOT EXISTS "inventory"."serial" (
    "id" TEXT NOT NULL, -- The serial number / IMEI / license key itself
    "stock_id" BIGINT NOT NULL,
    "status" "inventory"."status" NOT NULL DEFAULT 'Active',
    "date_created" TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "serial_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "serial_stock_id_fkey" FOREIGN KEY ("stock_id")
        REFERENCES "inventory"."stock" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "serial_stock_id_idx" ON "inventory"."serial" ("stock_id");
-- Partial index: quickly find available (unassigned) serial units for a stock record
CREATE INDEX IF NOT EXISTS "serial_active_stock_id_idx"
    ON "inventory"."serial" ("stock_id")
    WHERE "status" = 'Active';

-- Append-only audit log of every stock level change (restocks, sales, adjustments).
CREATE TABLE IF NOT EXISTS "inventory"."stock_history" (
    "id" BIGSERIAL NOT NULL,
    "stock_id" BIGINT NOT NULL,
    "change" BIGINT NOT NULL, -- Signed quantity delta (positive = restock, negative = sale/adjustment)
    "date_created" TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "stock_history_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "stock_history_stock_id_fkey" FOREIGN KEY ("stock_id")
        REFERENCES "inventory"."stock" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "stock_history_stock_id_idx" ON "inventory"."stock_history" ("stock_id");
CREATE INDEX IF NOT EXISTS "stock_history_date_created_idx" ON "inventory"."stock_history" ("date_created");
