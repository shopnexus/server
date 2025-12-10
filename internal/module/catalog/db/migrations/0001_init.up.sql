CREATE SCHEMA IF NOT EXISTS "catalog";

CREATE TYPE "catalog"."comment_ref_type" AS ENUM ('ProductSpu', 'Comment');
CREATE TYPE "catalog"."search_sync_ref_type" AS ENUM ('ProductSpu');

CREATE TABLE IF NOT EXISTS "catalog"."brand" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "code" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    CONSTRAINT "brand_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "catalog"."category" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" VARCHAR(100) NOT NULL,
    "description" TEXT NOT NULL,
    "parent_id" BIGINT,
    CONSTRAINT "category_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "catalog"."product_spu" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "slug" TEXT NOT NULL,
    "account_id" UUID NOT NULL,
    "category_id" UUID NOT NULL,
    "brand_id" UUID NOT NULL,
    "featured_sku_id" UUID,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    "is_active" BOOLEAN NOT NULL,
    "specifications" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_deleted" TIMESTAMPTZ(3),
    CONSTRAINT "product_spu_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "catalog"."product_sku" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "spu_id" UUID NOT NULL,
    "price" BIGINT NOT NULL,
    "can_combine" BOOLEAN NOT NULL,
    "attributes" JSONB NOT NULL,
    "package_details" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_deleted" TIMESTAMPTZ(3),
    CONSTRAINT "product_sku_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "catalog"."tag" (
    "id" VARCHAR(100) NOT NULL,
    "description" VARCHAR(255),
    CONSTRAINT "tag_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "catalog"."product_spu_tag" (
    "id" BIGSERIAL NOT NULL,
    "spu_id" UUID NOT NULL,
    "tag" VARCHAR(100) NOT NULL,
    CONSTRAINT "product_spu_tag_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "catalog"."comment" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "ref_type" "catalog"."comment_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "body" TEXT NOT NULL,
    "upvote" BIGINT NOT NULL DEFAULT 0,
    "downvote" BIGINT NOT NULL DEFAULT 0,
    "score" DOUBLE PRECISION NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "comment_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "catalog"."search_sync" (
    "id" BIGSERIAL NOT NULL,
    "ref_type" "catalog"."search_sync_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "is_stale_embedding" BOOLEAN NOT NULL DEFAULT true,
    "is_stale_metadata" BOOLEAN NOT NULL DEFAULT true,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "search_sync_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "brand_code_key" ON "catalog"."brand" ("code");
CREATE UNIQUE INDEX IF NOT EXISTS "category_name_key" ON "catalog"."category" ("name");
CREATE INDEX IF NOT EXISTS "category_parent_id_idx" ON "catalog"."category" ("parent_id");
CREATE UNIQUE INDEX IF NOT EXISTS "product_spu_slug_key" ON "catalog"."product_spu" ("slug");
CREATE UNIQUE INDEX IF NOT EXISTS "product_spu_featured_sku_id_key" ON "catalog"."product_spu" ("featured_sku_id");
CREATE INDEX IF NOT EXISTS "product_spu_account_id_idx" ON "catalog"."product_spu" ("account_id");
CREATE INDEX IF NOT EXISTS "product_spu_category_id_idx" ON "catalog"."product_spu" ("category_id");
CREATE INDEX IF NOT EXISTS "product_spu_brand_id_idx" ON "catalog"."product_spu" ("brand_id");
CREATE INDEX IF NOT EXISTS "product_sku_spu_id_idx" ON "catalog"."product_sku" ("spu_id");
CREATE UNIQUE INDEX IF NOT EXISTS "product_spu_tag_spu_id_tag_key" ON "catalog"."product_spu_tag" ("spu_id", "tag");
CREATE INDEX IF NOT EXISTS "search_sync_ref_type_ref_id_idx" ON "catalog"."search_sync" ("ref_type", "ref_id");
CREATE INDEX IF NOT EXISTS "search_sync_is_stale_embedding_idx" ON "catalog"."search_sync" ("is_stale_embedding");
CREATE INDEX IF NOT EXISTS "search_sync_is_stale_metadata_idx" ON "catalog"."search_sync" ("is_stale_metadata");
CREATE INDEX IF NOT EXISTS "search_sync_date_created_idx" ON "catalog"."search_sync" ("date_created");
CREATE UNIQUE INDEX IF NOT EXISTS "search_sync_ref_type_ref_id_key" ON "catalog"."search_sync" ("ref_type", "ref_id");

ALTER TABLE "catalog"."product_spu"
    ADD CONSTRAINT "product_spu_category_id_fkey"
    FOREIGN KEY ("category_id") REFERENCES "catalog"."category" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "catalog"."product_spu"
    ADD CONSTRAINT "product_spu_brand_id_fkey"
    FOREIGN KEY ("brand_id") REFERENCES "catalog"."brand" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "catalog"."product_sku"
    ADD CONSTRAINT "product_sku_spu_id_fkey"
    FOREIGN KEY ("spu_id") REFERENCES "catalog"."product_spu" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "catalog"."product_spu_tag"
    ADD CONSTRAINT "product_spu_tag_spu_id_fkey"
    FOREIGN KEY ("spu_id") REFERENCES "catalog"."product_spu" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "catalog"."product_spu_tag"
    ADD CONSTRAINT "product_spu_tag_tag_fkey"
    FOREIGN KEY ("tag") REFERENCES "catalog"."tag" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
