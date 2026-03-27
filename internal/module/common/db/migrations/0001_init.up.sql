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
CREATE TYPE "common"."resource_ref_type" AS ENUM ('ProductSpu', 'ProductSku', 'Brand', 'Refund', 'ReturnDispute', 'Comment');

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
    -- ID of the owning entity (product, brand, comment, etc.)
    "ref_id" UUID NOT NULL,
    -- Display order position among other resources for the same entity
    "order" INTEGER NOT NULL,
    CONSTRAINT "resource_reference_pkey" PRIMARY KEY ("id")
);

-- Registry of pluggable service integrations selectable at checkout or configuration time.
-- category groups related providers (e.g. 'payment', 'shipping').
-- provider and method identify the specific adapter implementation.
-- "order" controls display order within a category in the UI.
CREATE TABLE IF NOT EXISTS "common"."service_option" (
    -- Stable identifier for this option (e.g. 'stripe', 'vnpay', 'ghn')
    "id" VARCHAR(100) NOT NULL,
    -- Grouping key (e.g. 'payment', 'shipping', 'notification')
    "category" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    -- Backend adapter identifier (e.g. 'stripe', 'ghn')
    "provider" TEXT NOT NULL,
    -- Handler method within the provider adapter
    "method" TEXT NOT NULL,
    "is_active" BOOLEAN NOT NULL DEFAULT true,
    -- Display order within the category
    "order" INTEGER NOT NULL,
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
