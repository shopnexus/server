-- =============================================
-- Module: Promotion
-- Schema: promotion
-- Description: Discount and promotion engine. Supports multiple promotion
--              types (discounts, shipping discounts, bundles, BuyXGetY,
--              cashback), scoped to specific products/categories,
--              with optional recurring cron-based scheduling.
-- =============================================

CREATE SCHEMA IF NOT EXISTS "promotion";

-- Enums

-- The promotion mechanic applied at checkout
CREATE TYPE "promotion"."type" AS ENUM ('Discount', 'ShipDiscount', 'Bundle', 'BuyXGetY', 'Cashback');
-- Entity types that a promotion can target
CREATE TYPE "promotion"."ref_type" AS ENUM ('ProductSpu', 'ProductSku', 'Category');

-- Tables

-- Master promotion record. owner_id = NULL means platform-wide.
CREATE TABLE IF NOT EXISTS "promotion"."promotion" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    -- Coupon code entered by the buyer (unique, e.g. 'SUMMER20')
    "code" TEXT NOT NULL,
    -- NULL for platform promotions; seller account ID for seller promotions
    "owner_id" UUID,
    "type" "promotion"."type" NOT NULL,
    "title" TEXT NOT NULL,
    "description" TEXT,
    "is_active" BOOLEAN NOT NULL,
    -- When true, applied automatically at checkout without requiring a code entry
    "auto_apply" BOOLEAN NOT NULL,
    -- Logical grouping label (e.g. 'flash_sale', 'loyalty', 'bundle')
    "group" TEXT NOT NULL,
    -- Conflict resolution: higher priority promotions take precedence
    "priority" INTEGER NOT NULL DEFAULT 0,
    -- Type-specific rules and parameters; structure varies by promotion type
    "data" JSONB,
    "date_started" TIMESTAMPTZ(3) NOT NULL,
    -- NULL means the promotion runs indefinitely
    "date_ended" TIMESTAMPTZ(3),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "promotion_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "promotion_code_key" UNIQUE ("code")
);
-- Partial index for active promotion lookups (avoids scanning inactive rows)
CREATE INDEX IF NOT EXISTS "promotion_active_date_idx" ON "promotion"."promotion" ("is_active", "date_started", "date_ended") WHERE "is_active" = true;
-- Partial index for seller-owned promotion management
CREATE INDEX IF NOT EXISTS "promotion_owner_id_idx" ON "promotion"."promotion" ("owner_id") WHERE "owner_id" IS NOT NULL;

-- Scoping rules: links a promotion to specific products, SKUs, categories.
-- A promotion with no refs applies to all eligible items (global).
CREATE TABLE IF NOT EXISTS "promotion"."ref" (
    "id" BIGSERIAL NOT NULL,
    "promotion_id" UUID NOT NULL,
    "ref_type" "promotion"."ref_type" NOT NULL,
    -- ID of the targeted entity (SPU, SKU, category)
    "ref_id" UUID NOT NULL,
    CONSTRAINT "ref_pkey" PRIMARY KEY ("id"),
    -- Prevent duplicate scope entries for the same promotion + entity
    CONSTRAINT "ref_promotion_id_ref_type_ref_id_key" UNIQUE ("promotion_id", "ref_type", "ref_id"),
    CONSTRAINT "ref_promotion_id_fkey" FOREIGN KEY ("promotion_id")
        REFERENCES "promotion"."promotion" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
-- For looking up which promotions apply to a given entity
CREATE INDEX IF NOT EXISTS "ref_ref_type_ref_id_idx" ON "promotion"."ref" ("ref_type", "ref_id");

-- Recurring schedule for time-boxed flash sales or periodic activation.
CREATE TABLE IF NOT EXISTS "promotion"."schedule" (
    "id" BIGSERIAL NOT NULL,
    "promotion_id" UUID NOT NULL,
    -- IANA timezone identifier (e.g. 'Asia/Ho_Chi_Minh')
    "timezone" TEXT NOT NULL,
    -- Standard cron expression for recurrence (e.g. '0 9 * * 1' = every Monday 9am)
    "cron_rule" TEXT NOT NULL,
    -- How long (in minutes) the promotion stays active after each trigger
    "duration" INTEGER NOT NULL,
    -- Next scheduled activation; NULL until first schedule calculation
    "next_run_at" TIMESTAMPTZ(3),
    "last_run_at" TIMESTAMPTZ(3),
    CONSTRAINT "schedule_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "schedule_promotion_id_fkey" FOREIGN KEY ("promotion_id")
        REFERENCES "promotion"."promotion" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
-- Partial index for the scheduler to find upcoming runs efficiently
CREATE INDEX IF NOT EXISTS "schedule_next_run_idx" ON "promotion"."schedule" ("next_run_at") WHERE "next_run_at" IS NOT NULL;
