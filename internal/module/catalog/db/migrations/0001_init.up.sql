-- =============================================
-- Module: Catalog
-- Schema: catalog
-- Description: Product catalog including categories, SPUs (Standard
--              Product Units), SKUs (Stock Keeping Units), tags, comments,
--              and search/vector sync state.
-- =============================================

CREATE SCHEMA IF NOT EXISTS "catalog";

-- Enums

-- Entities a comment can be attached to (product or another comment for threading)
CREATE TYPE "catalog"."comment_ref_type" AS ENUM ('ProductSpu', 'Comment');
-- Entities tracked in the search sync queue
CREATE TYPE "catalog"."search_sync_ref_type" AS ENUM ('ProductSpu');

-- Tables

-- Hierarchical product category tree. parent_id = NULL means root category.
CREATE TABLE IF NOT EXISTS "catalog"."category" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" VARCHAR(100) NOT NULL,
    "description" TEXT NOT NULL,
    -- NULL for top-level categories; references another category row by its legacy BIGINT ID
    "parent_id" BIGINT,
    CONSTRAINT "category_pkey" PRIMARY KEY ("id")
);

-- SPU (Standard Product Unit): the canonical product definition shared across
-- all sellers. One SPU may have many SKUs (variants). account_id is the seller
-- who listed the product. featured_sku_id is the variant shown in listings.
-- specifications holds product-type-specific attribute definitions (JSONB).
CREATE TABLE IF NOT EXISTS "catalog"."product_spu" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    -- Human-readable sequential listing number
    "number" BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY,
    -- URL-friendly slug derived from name, must be globally unique
    "slug" TEXT NOT NULL,
    -- Seller account that owns this listing
    "account_id" UUID NOT NULL,
    "category_id" UUID NOT NULL,
    -- The variant displayed in search results and the product card
    "featured_sku_id" UUID,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    "is_active" BOOLEAN NOT NULL,
    -- Structured attribute schema specific to the product type
    "specifications" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Soft-delete timestamp; NULL means the product is live
    "date_deleted" TIMESTAMPTZ(3),
    CONSTRAINT "product_spu_pkey" PRIMARY KEY ("id")
);

-- SKU (Stock Keeping Unit): a specific purchasable variant of an SPU
-- (e.g. size=L, color=Red). price is in the smallest currency unit (e.g. cents/VND).
-- can_combine controls whether this SKU can be bundled with other SKUs in promotions.
-- attributes holds the variant dimension values (JSONB).
-- package_details holds weight/dimensions for shipping calculation.
CREATE TABLE IF NOT EXISTS "catalog"."product_sku" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "spu_id" UUID NOT NULL,
    -- Price in smallest currency unit (e.g. VND, cents)
    "price" BIGINT NOT NULL,
    -- Whether this SKU can participate in bundle promotions
    "can_combine" BOOLEAN NOT NULL,
    -- Variant attribute key/value pairs (e.g. {"size": "L", "color": "Red"})
    "attributes" JSONB NOT NULL,
    -- Physical packaging info for shipping (weight, dimensions, etc.)
    "package_details" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Soft-delete timestamp; NULL means the SKU is purchasable
    "date_deleted" TIMESTAMPTZ(3),
    CONSTRAINT "product_sku_pkey" PRIMARY KEY ("id")
);

-- Flat tag dictionary. id is the tag slug (e.g. 'eco-friendly', 'handmade').
CREATE TABLE IF NOT EXISTS "catalog"."tag" (
    "id" VARCHAR(100) NOT NULL,
    "description" VARCHAR(255),
    CONSTRAINT "tag_pkey" PRIMARY KEY ("id")
);

-- Many-to-many join between SPUs and tags.
CREATE TABLE IF NOT EXISTS "catalog"."product_spu_tag" (
    "id" BIGSERIAL NOT NULL,
    "spu_id" UUID NOT NULL,
    "tag" VARCHAR(100) NOT NULL,
    CONSTRAINT "product_spu_tag_pkey" PRIMARY KEY ("id")
);

-- User comments on products or replies to other comments (threaded via ref_type/ref_id).
-- score is a derived ranking value (e.g. Wilson score) from upvote/downvote counts.
CREATE TABLE IF NOT EXISTS "catalog"."comment" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    -- Optional reference to the order that purchased the product being reviewed (must not null when ref_type='ProductSpu')
    "order_id" UUID,
    -- 'ProductSpu' = top-level review; 'Comment' = reply to another comment
    "ref_type" "catalog"."comment_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "body" TEXT NOT NULL,
    "upvote" BIGINT NOT NULL DEFAULT 0,
    "downvote" BIGINT NOT NULL DEFAULT 0,
    -- Computed ranking score (e.g. upvote - downvote or Wilson lower bound)
    "score" DOUBLE PRECISION NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "comment_pkey" PRIMARY KEY ("id")
);

-- Sync queue for the vector search index (Milvus) and full-text search metadata.
-- A background worker polls this table for stale entries and reindexes them.
-- is_stale_embedding: the dense vector embedding needs regeneration.
-- is_stale_metadata: the keyword/filter metadata fields need updating.
CREATE TABLE IF NOT EXISTS "catalog"."search_sync" (
    "id" BIGSERIAL NOT NULL,
    "ref_type" "catalog"."search_sync_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    -- True when the product text has changed and the embedding must be regenerated
    "is_stale_embedding" BOOLEAN NOT NULL DEFAULT true,
    -- True when searchable metadata (price, category, etc.) has changed
    "is_stale_metadata" BOOLEAN NOT NULL DEFAULT true,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "search_sync_pkey" PRIMARY KEY ("id")
);

-- Indexes

CREATE UNIQUE INDEX IF NOT EXISTS "category_name_key" ON "catalog"."category" ("name");
CREATE INDEX IF NOT EXISTS "category_parent_id_idx" ON "catalog"."category" ("parent_id");
CREATE UNIQUE INDEX IF NOT EXISTS "product_spu_slug_key" ON "catalog"."product_spu" ("slug");
CREATE UNIQUE INDEX IF NOT EXISTS "product_spu_featured_sku_id_key" ON "catalog"."product_spu" ("featured_sku_id");
CREATE INDEX IF NOT EXISTS "product_spu_account_id_idx" ON "catalog"."product_spu" ("account_id");
CREATE INDEX IF NOT EXISTS "product_spu_category_id_idx" ON "catalog"."product_spu" ("category_id");
CREATE INDEX IF NOT EXISTS "product_sku_spu_id_idx" ON "catalog"."product_sku" ("spu_id");
CREATE UNIQUE INDEX IF NOT EXISTS "product_spu_tag_spu_id_tag_key" ON "catalog"."product_spu_tag" ("spu_id", "tag");
CREATE INDEX IF NOT EXISTS "search_sync_ref_type_ref_id_idx" ON "catalog"."search_sync" ("ref_type", "ref_id");
CREATE INDEX IF NOT EXISTS "search_sync_is_stale_embedding_idx" ON "catalog"."search_sync" ("is_stale_embedding");
CREATE INDEX IF NOT EXISTS "search_sync_is_stale_metadata_idx" ON "catalog"."search_sync" ("is_stale_metadata");
CREATE INDEX IF NOT EXISTS "search_sync_date_created_idx" ON "catalog"."search_sync" ("date_created");
-- Unique per entity to prevent duplicate sync rows
CREATE UNIQUE INDEX IF NOT EXISTS "search_sync_ref_type_ref_id_key" ON "catalog"."search_sync" ("ref_type", "ref_id");

-- Foreign keys

ALTER TABLE "catalog"."product_spu"
    ADD CONSTRAINT "product_spu_category_id_fkey"
    FOREIGN KEY ("category_id") REFERENCES "catalog"."category" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "catalog"."product_sku"
    ADD CONSTRAINT "product_sku_spu_id_fkey"
    FOREIGN KEY ("spu_id") REFERENCES "catalog"."product_spu" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "catalog"."product_spu_tag"
    ADD CONSTRAINT "product_spu_tag_spu_id_fkey"
    FOREIGN KEY ("spu_id") REFERENCES "catalog"."product_spu" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "catalog"."product_spu_tag"
    ADD CONSTRAINT "product_spu_tag_tag_fkey"
    FOREIGN KEY ("tag") REFERENCES "catalog"."tag" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
