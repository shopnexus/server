-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "account";

-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "analytic";

-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "catalog";

-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "common";

-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "inventory";

-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "order";

-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "promotion";

-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "system";

-- CreateEnum
CREATE TYPE "account"."type" AS ENUM ('Customer', 'Vendor');

-- CreateEnum
CREATE TYPE "account"."status" AS ENUM ('Active', 'Suspended');

-- CreateEnum
CREATE TYPE "account"."gender" AS ENUM ('Male', 'Female', 'Other');

-- CreateEnum
CREATE TYPE "account"."address_type" AS ENUM ('Home', 'Work');

-- CreateEnum
CREATE TYPE "analytic"."interaction_ref_type" AS ENUM ('Product', 'Category', 'Brand');

-- CreateEnum
CREATE TYPE "catalog"."comment_ref_type" AS ENUM ('ProductSpu', 'Comment');

-- CreateEnum
CREATE TYPE "common"."resource_ref_type" AS ENUM ('ProductSpu', 'ProductSku', 'Brand', 'Refund', 'ReturnDispute', 'Comment');

-- CreateEnum
CREATE TYPE "common"."status" AS ENUM ('Pending', 'Processing', 'Success', 'Canceled', 'Failed');

-- CreateEnum
CREATE TYPE "inventory"."stock_ref_type" AS ENUM ('ProductSku', 'Promotion');

-- CreateEnum
CREATE TYPE "inventory"."product_status" AS ENUM ('Active', 'Inactive', 'Sold', 'Damaged');

-- CreateEnum
CREATE TYPE "order"."refund_method" AS ENUM ('PickUp', 'DropOff');

-- CreateEnum
CREATE TYPE "order"."shipment_status" AS ENUM ('Pending', 'LabelCreated', 'InTransit', 'OutForDelivery', 'Delivered', 'Failed', 'Cancelled');

-- CreateEnum
CREATE TYPE "promotion"."type" AS ENUM ('Discount', 'ShipDiscount', 'Bundle', 'BuyXGetY', 'Cashback');

-- CreateEnum
CREATE TYPE "promotion"."ref_type" AS ENUM ('ProductSpu', 'ProductSku', 'Category', 'Brand');

-- CreateTable
CREATE TABLE "account"."account" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "type" "account"."type" NOT NULL,
    "status" "account"."status" NOT NULL DEFAULT 'Active',
    "phone" VARCHAR(50),
    "email" VARCHAR(255),
    "username" VARCHAR(100),
    "password" VARCHAR(255),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "account_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "account"."profile" (
    "id" UUID NOT NULL,
    "gender" "account"."gender",
    "name" VARCHAR(100),
    "date_of_birth" TIMESTAMP(3),
    "avatar_rs_id" UUID,
    "email_verified" BOOLEAN NOT NULL DEFAULT false,
    "phone_verified" BOOLEAN NOT NULL DEFAULT false,
    "default_contact_id" UUID,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "profile_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "account"."customer" (
    "id" UUID NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "customer_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "account"."vendor" (
    "id" UUID NOT NULL,
    "description" TEXT NOT NULL DEFAULT '',

    CONSTRAINT "vendor_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "account"."income_history" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "type" VARCHAR(50) NOT NULL,
    "income" BIGINT NOT NULL,
    "current_balance" BIGINT NOT NULL,
    "note" VARCHAR(100),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "income_history_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "account"."notification" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "type" VARCHAR(50) NOT NULL,
    "channel" VARCHAR(50) NOT NULL,
    "is_read" BOOLEAN NOT NULL DEFAULT false,
    "content" TEXT NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_sent" TIMESTAMPTZ(3),
    "date_scheduled" TIMESTAMPTZ(3),

    CONSTRAINT "notification_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "account"."contact" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "full_name" VARCHAR(100) NOT NULL,
    "phone" VARCHAR(30) NOT NULL,
    "phone_verified" BOOLEAN NOT NULL DEFAULT false,
    "address" VARCHAR(255) NOT NULL,
    "address_type" "account"."address_type" NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "contact_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "analytic"."interaction" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID,
    "session_id" TEXT,
    "event_type" TEXT NOT NULL,
    "ref_type" "analytic"."interaction_ref_type" NOT NULL,
    "ref_id" TEXT NOT NULL,
    "metadata" JSONB,
    "user_agent" TEXT,
    "ip_address" TEXT,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "interaction_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "catalog"."brand" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "code" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,

    CONSTRAINT "brand_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "catalog"."category" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" VARCHAR(100) NOT NULL,
    "description" TEXT NOT NULL,
    "parent_id" BIGINT,

    CONSTRAINT "category_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "catalog"."product_spu" (
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

-- CreateTable
CREATE TABLE "catalog"."product_sku" (
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

-- CreateTable
CREATE TABLE "catalog"."tag" (
    "id" VARCHAR(100) NOT NULL,
    "description" VARCHAR(255),

    CONSTRAINT "tag_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "catalog"."product_spu_tag" (
    "id" BIGSERIAL NOT NULL,
    "spu_id" UUID NOT NULL,
    "tag" VARCHAR(100) NOT NULL,

    CONSTRAINT "product_spu_tag_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "catalog"."comment" (
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

-- CreateTable
CREATE TABLE "catalog"."search_sync" (
    "id" BIGSERIAL NOT NULL,
    "ref_type" VARCHAR(100) NOT NULL,
    "ref_id" UUID NOT NULL,
    "is_stale_embedding" BOOLEAN NOT NULL DEFAULT true,
    "is_stale_metadata" BOOLEAN NOT NULL DEFAULT true,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "search_sync_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "common"."resource" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "uploaded_by" UUID,
    "provider" TEXT NOT NULL,
    "object_key" VARCHAR(2048) NOT NULL,
    "mime" VARCHAR(100) NOT NULL,
    "size" BIGINT NOT NULL,
    "metadata" JSONB NOT NULL,
    "checksum" TEXT,
    "status" "common"."status" NOT NULL DEFAULT 'Pending',
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "resource_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "common"."resource_reference" (
    "id" BIGSERIAL NOT NULL,
    "rs_id" UUID NOT NULL,
    "ref_type" "common"."resource_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "order" INTEGER NOT NULL,

    CONSTRAINT "resource_reference_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "common"."service_option" (
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

-- CreateTable
CREATE TABLE "inventory"."sku_serial" (
    "id" TEXT NOT NULL,
    "sku_id" UUID NOT NULL,
    "status" "inventory"."product_status" NOT NULL DEFAULT 'Active',
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "sku_serial_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "inventory"."stock" (
    "id" BIGSERIAL NOT NULL,
    "ref_type" "inventory"."stock_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "stock" BIGINT NOT NULL,
    "taken" BIGINT NOT NULL DEFAULT 0,
    "serial_required" BOOLEAN NOT NULL DEFAULT false,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "stock_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "inventory"."stock_history" (
    "id" BIGSERIAL NOT NULL,
    "stock_id" BIGINT NOT NULL,
    "change" BIGINT NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "stock_history_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "order"."cart_item" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "sku_id" UUID NOT NULL,
    "quantity" BIGINT NOT NULL,

    CONSTRAINT "cart_item_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "order"."payment" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "option" TEXT NOT NULL,
    "status" "common"."status" NOT NULL DEFAULT 'Pending',
    "amount" BIGINT NOT NULL,
    "data" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_paid" TIMESTAMPTZ(3),
    "date_expired" TIMESTAMPTZ(3) NOT NULL,

    CONSTRAINT "payment_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "order"."order" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "customer_id" UUID NOT NULL,
    "vendor_id" UUID NOT NULL,
    "payment_id" BIGINT NOT NULL,
    "shipment_id" UUID NOT NULL,
    "confirmed_by_id" UUID,
    "status" "common"."status" NOT NULL DEFAULT 'Pending',
    "address" TEXT NOT NULL,
    "product_cost" BIGINT NOT NULL,
    "product_discount" BIGINT NOT NULL,
    "ship_cost" BIGINT NOT NULL,
    "ship_discount" BIGINT NOT NULL,
    "total" BIGINT NOT NULL,
    "note" TEXT,
    "data" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "order_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "order"."item" (
    "id" BIGSERIAL NOT NULL,
    "order_id" UUID NOT NULL,
    "sku_id" UUID NOT NULL,
    "sku_name" TEXT NOT NULL,
    "quantity" BIGINT NOT NULL,
    "unit_price" BIGINT NOT NULL,
    "note" TEXT,
    "serial_ids" JSONB,

    CONSTRAINT "item_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "order"."refund" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "order_id" UUID NOT NULL,
    "confirmed_by_id" UUID,
    "shipment_id" UUID,
    "method" "order"."refund_method" NOT NULL,
    "status" "common"."status" NOT NULL DEFAULT 'Pending',
    "reason" TEXT NOT NULL,
    "address" TEXT,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "refund_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "order"."refund_dispute" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "refund_id" UUID NOT NULL,
    "issued_by_id" UUID NOT NULL,
    "reason" TEXT NOT NULL,
    "status" "common"."status" NOT NULL DEFAULT 'Pending',
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "refund_dispute_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "order"."shipment" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "option" TEXT NOT NULL,
    "tracking_code" TEXT,
    "status" "order"."shipment_status" NOT NULL DEFAULT 'Pending',
    "label_url" TEXT,
    "cost" BIGINT NOT NULL,
    "new_cost" BIGINT NOT NULL,
    "date_eta" TIMESTAMPTZ(3) NOT NULL,
    "from_address" TEXT NOT NULL,
    "to_address" TEXT NOT NULL,
    "weight_grams" INTEGER NOT NULL,
    "length_cm" INTEGER NOT NULL,
    "width_cm" INTEGER NOT NULL,
    "height_cm" INTEGER NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "shipment_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "promotion"."promotion" (
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

-- CreateTable
CREATE TABLE "promotion"."ref" (
    "id" BIGSERIAL NOT NULL,
    "promotion_id" UUID NOT NULL,
    "ref_type" "promotion"."ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,

    CONSTRAINT "ref_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "promotion"."schedule" (
    "id" BIGSERIAL NOT NULL,
    "promotion_id" UUID NOT NULL,
    "timezone" TEXT NOT NULL,
    "cron_rule" TEXT NOT NULL,
    "duration" INTEGER NOT NULL,
    "next_run_at" TIMESTAMPTZ(3),
    "last_run_at" TIMESTAMPTZ(3),

    CONSTRAINT "schedule_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "promotion"."discount" (
    "id" UUID NOT NULL,
    "min_spend" BIGINT NOT NULL,
    "max_discount" BIGINT NOT NULL,
    "discount_percent" DOUBLE PRECISION,
    "discount_price" BIGINT,

    CONSTRAINT "discount_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "system"."outbox_event" (
    "id" BIGSERIAL NOT NULL,
    "topic" VARCHAR(100) NOT NULL,
    "data" JSONB NOT NULL,
    "processed" BOOLEAN NOT NULL DEFAULT false,
    "date_processed" TIMESTAMPTZ(3),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "outbox_event_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "account_phone_key" ON "account"."account"("phone");

-- CreateIndex
CREATE UNIQUE INDEX "account_email_key" ON "account"."account"("email");

-- CreateIndex
CREATE UNIQUE INDEX "account_username_key" ON "account"."account"("username");

-- CreateIndex
CREATE UNIQUE INDEX "profile_avatar_rs_id_key" ON "account"."profile"("avatar_rs_id");

-- CreateIndex
CREATE UNIQUE INDEX "profile_default_contact_id_key" ON "account"."profile"("default_contact_id");

-- CreateIndex
CREATE INDEX "vendor_id_idx" ON "account"."vendor"("id");

-- CreateIndex
CREATE INDEX "income_history_account_id_idx" ON "account"."income_history"("account_id");

-- CreateIndex
CREATE INDEX "income_history_type_idx" ON "account"."income_history"("type");

-- CreateIndex
CREATE INDEX "income_history_date_created_idx" ON "account"."income_history"("date_created");

-- CreateIndex
CREATE INDEX "notification_account_id_idx" ON "account"."notification"("account_id");

-- CreateIndex
CREATE INDEX "notification_type_idx" ON "account"."notification"("type");

-- CreateIndex
CREATE INDEX "notification_channel_idx" ON "account"."notification"("channel");

-- CreateIndex
CREATE INDEX "notification_date_created_idx" ON "account"."notification"("date_created");

-- CreateIndex
CREATE INDEX "contact_account_id_idx" ON "account"."contact"("account_id");

-- CreateIndex
CREATE INDEX "interaction_account_id_idx" ON "analytic"."interaction"("account_id");

-- CreateIndex
CREATE INDEX "interaction_session_id_idx" ON "analytic"."interaction"("session_id");

-- CreateIndex
CREATE INDEX "interaction_event_type_idx" ON "analytic"."interaction"("event_type");

-- CreateIndex
CREATE INDEX "interaction_ref_type_ref_id_idx" ON "analytic"."interaction"("ref_type", "ref_id");

-- CreateIndex
CREATE UNIQUE INDEX "brand_code_key" ON "catalog"."brand"("code");

-- CreateIndex
CREATE UNIQUE INDEX "category_name_key" ON "catalog"."category"("name");

-- CreateIndex
CREATE INDEX "category_parent_id_idx" ON "catalog"."category"("parent_id");

-- CreateIndex
CREATE UNIQUE INDEX "product_spu_slug_key" ON "catalog"."product_spu"("slug");

-- CreateIndex
CREATE UNIQUE INDEX "product_spu_featured_sku_id_key" ON "catalog"."product_spu"("featured_sku_id");

-- CreateIndex
CREATE INDEX "product_spu_account_id_idx" ON "catalog"."product_spu"("account_id");

-- CreateIndex
CREATE INDEX "product_spu_category_id_idx" ON "catalog"."product_spu"("category_id");

-- CreateIndex
CREATE INDEX "product_spu_brand_id_idx" ON "catalog"."product_spu"("brand_id");

-- CreateIndex
CREATE INDEX "product_sku_spu_id_idx" ON "catalog"."product_sku"("spu_id");

-- CreateIndex
CREATE UNIQUE INDEX "product_spu_tag_spu_id_tag_key" ON "catalog"."product_spu_tag"("spu_id", "tag");

-- CreateIndex
CREATE INDEX "search_sync_ref_type_ref_id_idx" ON "catalog"."search_sync"("ref_type", "ref_id");

-- CreateIndex
CREATE INDEX "search_sync_is_stale_embedding_idx" ON "catalog"."search_sync"("is_stale_embedding");

-- CreateIndex
CREATE INDEX "search_sync_is_stale_metadata_idx" ON "catalog"."search_sync"("is_stale_metadata");

-- CreateIndex
CREATE INDEX "search_sync_date_created_idx" ON "catalog"."search_sync"("date_created");

-- CreateIndex
CREATE UNIQUE INDEX "search_sync_ref_type_ref_id_key" ON "catalog"."search_sync"("ref_type", "ref_id");

-- CreateIndex
CREATE UNIQUE INDEX "resource_provider_object_key_key" ON "common"."resource"("provider", "object_key");

-- CreateIndex
CREATE INDEX "service_option_category_provider_idx" ON "common"."service_option"("category", "provider");

-- CreateIndex
CREATE INDEX "sku_serial_sku_id_idx" ON "inventory"."sku_serial"("sku_id");

-- CreateIndex
CREATE UNIQUE INDEX "stock_ref_id_ref_type_key" ON "inventory"."stock"("ref_id", "ref_type");

-- CreateIndex
CREATE INDEX "stock_history_stock_id_idx" ON "inventory"."stock_history"("stock_id");

-- CreateIndex
CREATE INDEX "stock_history_date_created_idx" ON "inventory"."stock_history"("date_created");

-- CreateIndex
CREATE UNIQUE INDEX "cart_item_account_id_sku_id_key" ON "order"."cart_item"("account_id", "sku_id");

-- CreateIndex
CREATE UNIQUE INDEX "order_payment_id_key" ON "order"."order"("payment_id");

-- CreateIndex
CREATE INDEX "order_vendor_id_idx" ON "order"."order"("vendor_id");

-- CreateIndex
CREATE INDEX "item_order_id_idx" ON "order"."item"("order_id");

-- CreateIndex
CREATE INDEX "item_sku_id_idx" ON "order"."item"("sku_id");

-- CreateIndex
CREATE UNIQUE INDEX "item_order_id_sku_id_key" ON "order"."item"("order_id", "sku_id");

-- CreateIndex
CREATE INDEX "refund_account_id_idx" ON "order"."refund"("account_id");

-- CreateIndex
CREATE INDEX "refund_order_id_idx" ON "order"."refund"("order_id");

-- CreateIndex
CREATE INDEX "refund_confirmed_by_id_idx" ON "order"."refund"("confirmed_by_id");

-- CreateIndex
CREATE INDEX "refund_shipment_id_idx" ON "order"."refund"("shipment_id");

-- CreateIndex
CREATE INDEX "refund_dispute_refund_id_idx" ON "order"."refund_dispute"("refund_id");

-- CreateIndex
CREATE INDEX "refund_dispute_issued_by_id_idx" ON "order"."refund_dispute"("issued_by_id");

-- CreateIndex
CREATE UNIQUE INDEX "promotion_code_key" ON "promotion"."promotion"("code");

-- CreateIndex
CREATE UNIQUE INDEX "ref_promotion_id_ref_type_ref_id_key" ON "promotion"."ref"("promotion_id", "ref_type", "ref_id");

-- CreateIndex
CREATE INDEX "outbox_event_date_created_idx" ON "system"."outbox_event"("date_created");

-- AddForeignKey
ALTER TABLE "account"."profile" ADD CONSTRAINT "profile_default_contact_id_fkey" FOREIGN KEY ("default_contact_id") REFERENCES "account"."contact"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "account"."profile" ADD CONSTRAINT "profile_id_fkey" FOREIGN KEY ("id") REFERENCES "account"."account"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "account"."profile" ADD CONSTRAINT "profile_avatar_rs_id_fkey" FOREIGN KEY ("avatar_rs_id") REFERENCES "common"."resource"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "account"."customer" ADD CONSTRAINT "customer_id_fkey" FOREIGN KEY ("id") REFERENCES "account"."account"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "account"."vendor" ADD CONSTRAINT "vendor_id_fkey" FOREIGN KEY ("id") REFERENCES "account"."account"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "account"."income_history" ADD CONSTRAINT "income_history_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."vendor"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "account"."notification" ADD CONSTRAINT "notification_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."account"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "account"."contact" ADD CONSTRAINT "contact_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."account"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "analytic"."interaction" ADD CONSTRAINT "interaction_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."customer"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "catalog"."product_spu" ADD CONSTRAINT "product_spu_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."vendor"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "catalog"."product_spu" ADD CONSTRAINT "product_spu_category_id_fkey" FOREIGN KEY ("category_id") REFERENCES "catalog"."category"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "catalog"."product_spu" ADD CONSTRAINT "product_spu_brand_id_fkey" FOREIGN KEY ("brand_id") REFERENCES "catalog"."brand"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "catalog"."product_sku" ADD CONSTRAINT "product_sku_spu_id_fkey" FOREIGN KEY ("spu_id") REFERENCES "catalog"."product_spu"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "catalog"."product_spu_tag" ADD CONSTRAINT "product_spu_tag_spu_id_fkey" FOREIGN KEY ("spu_id") REFERENCES "catalog"."product_spu"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "catalog"."product_spu_tag" ADD CONSTRAINT "product_spu_tag_tag_fkey" FOREIGN KEY ("tag") REFERENCES "catalog"."tag"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "catalog"."comment" ADD CONSTRAINT "comment_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."account"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "common"."resource" ADD CONSTRAINT "resource_uploaded_by_fkey" FOREIGN KEY ("uploaded_by") REFERENCES "account"."account"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "common"."resource_reference" ADD CONSTRAINT "resource_reference_rs_id_fkey" FOREIGN KEY ("rs_id") REFERENCES "common"."resource"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "inventory"."sku_serial" ADD CONSTRAINT "sku_serial_sku_id_fkey" FOREIGN KEY ("sku_id") REFERENCES "catalog"."product_sku"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "inventory"."stock_history" ADD CONSTRAINT "stock_history_stock_id_fkey" FOREIGN KEY ("stock_id") REFERENCES "inventory"."stock"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."cart_item" ADD CONSTRAINT "cart_item_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."customer"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."cart_item" ADD CONSTRAINT "cart_item_sku_id_fkey" FOREIGN KEY ("sku_id") REFERENCES "catalog"."product_sku"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."payment" ADD CONSTRAINT "payment_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."customer"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."payment" ADD CONSTRAINT "payment_option_fkey" FOREIGN KEY ("option") REFERENCES "common"."service_option"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."order" ADD CONSTRAINT "order_customer_id_fkey" FOREIGN KEY ("customer_id") REFERENCES "account"."customer"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."order" ADD CONSTRAINT "order_shipment_id_fkey" FOREIGN KEY ("shipment_id") REFERENCES "order"."shipment"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."order" ADD CONSTRAINT "order_confirmed_by_id_fkey" FOREIGN KEY ("confirmed_by_id") REFERENCES "account"."vendor"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."order" ADD CONSTRAINT "order_vendor_id_fkey" FOREIGN KEY ("vendor_id") REFERENCES "account"."vendor"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."order" ADD CONSTRAINT "order_payment_id_fkey" FOREIGN KEY ("payment_id") REFERENCES "order"."payment"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."item" ADD CONSTRAINT "item_order_id_fkey" FOREIGN KEY ("order_id") REFERENCES "order"."order"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."item" ADD CONSTRAINT "item_sku_id_fkey" FOREIGN KEY ("sku_id") REFERENCES "catalog"."product_sku"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."refund" ADD CONSTRAINT "refund_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."customer"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."refund" ADD CONSTRAINT "refund_order_id_fkey" FOREIGN KEY ("order_id") REFERENCES "order"."order"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."refund" ADD CONSTRAINT "refund_confirmed_by_id_fkey" FOREIGN KEY ("confirmed_by_id") REFERENCES "account"."vendor"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."refund" ADD CONSTRAINT "refund_shipment_id_fkey" FOREIGN KEY ("shipment_id") REFERENCES "order"."shipment"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."refund_dispute" ADD CONSTRAINT "refund_dispute_refund_id_fkey" FOREIGN KEY ("refund_id") REFERENCES "order"."refund"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."refund_dispute" ADD CONSTRAINT "refund_dispute_issued_by_id_fkey" FOREIGN KEY ("issued_by_id") REFERENCES "account"."vendor"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "order"."shipment" ADD CONSTRAINT "shipment_option_fkey" FOREIGN KEY ("option") REFERENCES "common"."service_option"("id") ON DELETE NO ACTION ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "promotion"."promotion" ADD CONSTRAINT "promotion_owner_id_fkey" FOREIGN KEY ("owner_id") REFERENCES "account"."vendor"("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "promotion"."ref" ADD CONSTRAINT "ref_promotion_id_fkey" FOREIGN KEY ("promotion_id") REFERENCES "promotion"."promotion"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "promotion"."schedule" ADD CONSTRAINT "schedule_promotion_id_fkey" FOREIGN KEY ("promotion_id") REFERENCES "promotion"."promotion"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "promotion"."discount" ADD CONSTRAINT "discount_id_fkey" FOREIGN KEY ("id") REFERENCES "promotion"."promotion"("id") ON DELETE CASCADE ON UPDATE CASCADE;

