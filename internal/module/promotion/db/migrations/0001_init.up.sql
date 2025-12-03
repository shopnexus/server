CREATE SCHEMA IF NOT EXISTS "promotion";

CREATE TYPE "promotion"."type" AS ENUM ('Discount', 'ShipDiscount', 'Bundle', 'BuyXGetY', 'Cashback');
CREATE TYPE "promotion"."ref_type" AS ENUM ('ProductSpu', 'ProductSku', 'Category', 'Brand');

CREATE TABLE IF NOT EXISTS "promotion"."promotion" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "code" TEXT NOT NULL,
    "owner_id" UUID,
    "type" "promotion"."type" NOT NULL,
    "title" TEXT NOT NULL,
    "description" TEXT,
    "is_active" BOOLEAN NOT NULL,
    "auto_apply" BOOLEAN NOT NULL,
    "date_started" TIMESTAMPTZ(3) NOT NULL,
    "date_ended" TIMESTAMPTZ(3),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "promotion_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "promotion"."ref" (
    "id" BIGSERIAL NOT NULL,
    "promotion_id" UUID NOT NULL,
    "ref_type" "promotion"."ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    CONSTRAINT "ref_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "promotion"."schedule" (
    "id" BIGSERIAL NOT NULL,
    "promotion_id" UUID NOT NULL,
    "timezone" TEXT NOT NULL,
    "cron_rule" TEXT NOT NULL,
    "duration" INTEGER NOT NULL,
    "next_run_at" TIMESTAMPTZ(3),
    "last_run_at" TIMESTAMPTZ(3),
    CONSTRAINT "schedule_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "promotion"."discount" (
    "id" UUID NOT NULL,
    "min_spend" BIGINT NOT NULL,
    "max_discount" BIGINT NOT NULL,
    "discount_percent" DOUBLE PRECISION,
    "discount_price" BIGINT,
    CONSTRAINT "discount_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "promotion_code_key" ON "promotion"."promotion" ("code");
CREATE UNIQUE INDEX IF NOT EXISTS "ref_promotion_id_ref_type_ref_id_key" ON "promotion"."ref" ("promotion_id", "ref_type", "ref_id");

ALTER TABLE "promotion"."ref"
    ADD CONSTRAINT "ref_promotion_id_fkey"
    FOREIGN KEY ("promotion_id") REFERENCES "promotion"."promotion" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "promotion"."schedule"
    ADD CONSTRAINT "schedule_promotion_id_fkey"
    FOREIGN KEY ("promotion_id") REFERENCES "promotion"."promotion" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "promotion"."discount"
    ADD CONSTRAINT "discount_id_fkey"
    FOREIGN KEY ("id") REFERENCES "promotion"."promotion" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

