-- =============================================
-- Module: Common
-- Schema: common
-- Description: Cross-module shared infrastructure: file/media resources,
--              resource-to-entity references, and pluggable service option registry
--              (payment providers, shipping providers, etc.).
-- =============================================

CREATE SCHEMA IF NOT EXISTS "common";

-- Enums

-- Entity types that can have media attachments
CREATE TYPE "common"."resource_ref_type" AS ENUM ('ProductSpu', 'ProductSku', 'Refund', 'ReturnDispute', 'Comment');

-- Tables

-- Uploaded file/media record. provider identifies the storage backend
-- (e.g. 'S3', 'Local'). object_key is the path within that provider.
-- checksum is used for deduplication and integrity verification.
CREATE TABLE IF NOT EXISTS "common"."resource" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    -- Account that uploaded the file; NULL for system-generated resources
    "uploaded_by" UUID,
    -- Storage backend identifier (e.g. 'S3', 'Local', 'MinIO')
    "provider" TEXT NOT NULL,
    -- Provider-specific path or key (up to 2048 chars for S3 compatibility)
    "object_key" VARCHAR(2048) NOT NULL,
    -- MIME type (e.g. 'image/jpeg', 'application/pdf')
    "mime" VARCHAR(100) NOT NULL,
    -- File size in bytes
    "size" BIGINT NOT NULL,
    -- Provider-specific metadata (dimensions, duration, CDN URL, etc.)
    "metadata" JSONB NOT NULL,
    -- Optional content hash for deduplication
    "checksum" TEXT,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "resource_pkey" PRIMARY KEY ("id")
);

-- Associates a resource (file) with a domain entity.
-- "order" controls display ordering when an entity has multiple media files.
-- This indirection allows a single file to be referenced by multiple entities.
CREATE TABLE IF NOT EXISTS "common"."resource_reference" (
    "id" BIGSERIAL NOT NULL,
    -- The resource being referenced
    "rs_id" UUID NOT NULL,
    "ref_type" "common"."resource_ref_type" NOT NULL,
    -- ID of the owning entity (product, comment, etc.)
    "ref_id" UUID NOT NULL,
    -- Display order position among other resources for the same entity
    "order" INTEGER NOT NULL,
    CONSTRAINT "resource_reference_pkey" PRIMARY KEY ("id")
);

-- Registry of pluggable service integrations selectable at checkout or configuration time.
-- category groups related providers (e.g. 'payment', 'transport').
-- provider and method identify the specific adapter implementation.
-- "priority" controls display priority within a category in the UI.
CREATE TABLE IF NOT EXISTS "common"."service_option" (
    -- Stable identifier for this option (e.g. 'stripe-xxx', 'vnpay-qr|bank|xxx', 'ghn-xxx')
    "id" VARCHAR(100) NOT NULL,
    -- Grouping key (e.g. 'payment', 'transport', ...)
    "category" TEXT NOT NULL,
    -- Backend adapter identifier (e.g. 'stripe', 'ghn')
    "provider" TEXT NOT NULL,
    "is_active" BOOLEAN NOT NULL DEFAULT true,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    "priority" INTEGER NOT NULL,
    "config" JSONB NOT NULL,
    "logo_rs_id" UUID,
    CONSTRAINT "service_option_pkey" PRIMARY KEY ("id")
);

-- Indexes

-- Prevent duplicate file uploads to the same provider path
CREATE UNIQUE INDEX IF NOT EXISTS "resource_provider_object_key_key" ON "common"."resource" ("provider", "object_key");
CREATE INDEX IF NOT EXISTS "service_option_category_provider_idx" ON "common"."service_option" ("category", "provider");

-- Foreign keys

ALTER TABLE "common"."resource_reference"
    ADD CONSTRAINT "resource_reference_rs_id_fkey"
    FOREIGN KEY ("rs_id") REFERENCES "common"."resource" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "common"."service_option"
    ADD CONSTRAINT "service_option_logo_rs_id_fkey"
    FOREIGN KEY ("logo_rs_id") REFERENCES "common"."resource" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- =============================================
-- EXCHANGE RATES
-- =============================================
-- Exchange rates fetched from Frankfurter by common.SetupExchangeCron.
-- base is always USD in current deployment; (base, target) PK keeps
-- schema flexible for future multi-base storage.
CREATE TABLE IF NOT EXISTS "common"."exchange_rate" (
    "base"         VARCHAR(3)      NOT NULL,
    "target"       VARCHAR(3)      NOT NULL,
    "rate"         NUMERIC(20, 10) NOT NULL,
    "fetched_at"   TIMESTAMPTZ     NOT NULL,
    "date_updated" TIMESTAMPTZ     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "exchange_rate_pkey" PRIMARY KEY ("base", "target"),
    CONSTRAINT "exchange_rate_base_format_chk"   CHECK ("base"   ~ '^[A-Z]{3}$'),
    CONSTRAINT "exchange_rate_target_format_chk" CHECK ("target" ~ '^[A-Z]{3}$'),
    CONSTRAINT "exchange_rate_rate_positive_chk" CHECK ("rate" > 0)
);

CREATE INDEX IF NOT EXISTS "exchange_rate_target_idx"
    ON "common"."exchange_rate" ("target");

-- Seed conservative fallback rates so FE has data before first cron tick.
-- fetched_at = epoch → "stale" tooltip shows until cron refresh.
INSERT INTO "common"."exchange_rate" (base, target, rate, fetched_at) VALUES
    ('USD', 'VND', 25000,   '1970-01-01'::timestamptz),
    ('USD', 'JPY', 155,     '1970-01-01'::timestamptz),
    ('USD', 'KRW', 1350,    '1970-01-01'::timestamptz),
    ('USD', 'EUR', 0.92,    '1970-01-01'::timestamptz),
    ('USD', 'GBP', 0.78,    '1970-01-01'::timestamptz),
    ('USD', 'CNY', 7.3,     '1970-01-01'::timestamptz),
    ('USD', 'SGD', 1.35,    '1970-01-01'::timestamptz),
    ('USD', 'THB', 35,      '1970-01-01'::timestamptz),
    ('USD', 'AUD', 1.52,    '1970-01-01'::timestamptz)
ON CONFLICT (base, target) DO NOTHING;
