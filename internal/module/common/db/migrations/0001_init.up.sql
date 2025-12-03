CREATE SCHEMA IF NOT EXISTS "common";

CREATE TYPE "common"."resource_ref_type" AS ENUM ('ProductSpu', 'ProductSku', 'Brand', 'Refund', 'ReturnDispute', 'Comment');

CREATE TABLE IF NOT EXISTS "common"."resource" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "uploaded_by" UUID,
    "provider" TEXT NOT NULL,
    "object_key" VARCHAR(2048) NOT NULL,
    "mime" VARCHAR(100) NOT NULL,
    "size" BIGINT NOT NULL,
    "metadata" JSONB NOT NULL,
    "checksum" TEXT,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "resource_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "common"."resource_reference" (
    "id" BIGSERIAL NOT NULL,
    "rs_id" UUID NOT NULL,
    "ref_type" "common"."resource_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "order" INTEGER NOT NULL,
    CONSTRAINT "resource_reference_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "common"."service_option" (
    "id" VARCHAR(100) NOT NULL,
    "category" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    "provider" TEXT NOT NULL,
    "method" TEXT NOT NULL,
    "is_active" BOOLEAN NOT NULL DEFAULT true,
    "order" INTEGER NOT NULL,
    CONSTRAINT "service_option_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "resource_provider_object_key_key" ON "common"."resource" ("provider", "object_key");
CREATE INDEX IF NOT EXISTS "service_option_category_provider_idx" ON "common"."service_option" ("category", "provider");

ALTER TABLE "common"."resource_reference"
    ADD CONSTRAINT "resource_reference_rs_id_fkey"
    FOREIGN KEY ("rs_id") REFERENCES "common"."resource" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

