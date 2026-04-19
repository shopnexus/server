-- =============================================
-- Module: Order
-- Schema: order
-- Description: Shopping cart, checkout items, payments, transport/delivery,
--              orders, refunds, and refund disputes. Any account can be
--              buyer (buyer_id) or seller (seller_id) within a single order.
-- =============================================

CREATE SCHEMA IF NOT EXISTS "order";

-- Enums

-- How the buyer returns items for a refund
CREATE TYPE "order"."refund_method" AS ENUM ('PickUp', 'DropOff');
-- Delivery lifecycle states
CREATE TYPE "order"."transport_status" AS ENUM ('Pending', 'LabelCreated', 'InTransit', 'OutForDelivery', 'Delivered', 'Failed', 'Cancelled');
-- Generic status used by payment, order, refund, and refund_dispute tables
CREATE TYPE "order"."status" AS ENUM ('Pending', 'Processing', 'Success', 'Cancelled', 'Failed');

-- Tables

-- Flat shopping cart: one row per (account, SKU) pair. No timestamps — cart items
-- are transient. A unique constraint prevents duplicate SKU rows per account.
CREATE TABLE IF NOT EXISTS "order"."cart_item" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "sku_id" UUID NOT NULL,
    "quantity" BIGINT NOT NULL,
    CONSTRAINT "cart_item_pkey" PRIMARY KEY ("id")
);

-- Payment session record. amount is in the smallest currency unit.
-- option references a common.service_option id (e.g. 'vnpay', 'stripe').
-- data stores the provider-specific payment intent/token.
-- date_expired is set at creation; unpaid sessions past this are voided.
CREATE TABLE IF NOT EXISTS "order"."payment" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    -- References common.service_option.id (payment provider)
    "option" TEXT NOT NULL,
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
    -- Total amount to collect, in smallest currency unit
    "amount" BIGINT NOT NULL,
    -- Provider-specific payment intent, token, or QR code data
    "data" JSONB NOT NULL,
    "payment_method_id" UUID,
    "buyer_currency" VARCHAR(3) NOT NULL DEFAULT 'VND',
    "seller_currency" VARCHAR(3) NOT NULL DEFAULT 'VND',
    "exchange_rate" NUMERIC NOT NULL DEFAULT 1,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_paid" TIMESTAMPTZ(3),
    -- Payment session expiry; unpaid after this timestamp are voided
    "date_expired" TIMESTAMPTZ(3) NOT NULL,
    CONSTRAINT "payment_pkey" PRIMARY KEY ("id")
);

-- Transport/delivery record. option references common.service_option (shipping provider).
-- data stores provider-specific tracking data (label URL, tracking number, etc.).
-- cost is the shipping fee charged to the buyer.
CREATE TABLE IF NOT EXISTS "order"."transport" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    -- References common.service_option.id (shipping provider)
    "option" TEXT NOT NULL,
    "status" "order"."transport_status" DEFAULT 'Pending',
    -- Shipping fee in smallest currency unit
    "cost" BIGINT NOT NULL DEFAULT 0,
    -- Provider-specific data (tracking number, label URL, webhook events, etc.)
    "data" JSONB NOT NULL DEFAULT '{}',
    "date_created" TIMESTAMPTZ(3) DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "transport_pkey" PRIMARY KEY ("id")
);

-- Order created when a seller confirms pending items. Groups confirmed items from
-- the same seller into a single fulfillable unit. product_discount is the total
-- promotion savings; total = product_cost - product_discount + transport_cost.
-- confirmed_by_id is the account (seller or admin) who confirmed the order.
-- Pay-first: payment lives on items, not orders.
CREATE TABLE IF NOT EXISTS "order"."order" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    -- The account that purchased (buyer)
    "buyer_id" UUID NOT NULL,
    -- The account fulfilling the order (seller)
    "seller_id" UUID NOT NULL,
    -- NULL until a shipping label is created
    "transport_id" UUID,
    -- Account that confirmed the order (usually the seller)
    "confirmed_by_id" UUID,
    -- Snapshot of the delivery address at order time
    "address" TEXT NOT NULL,
    -- Sum of item unit prices × quantities before discounts
    "product_cost" BIGINT NOT NULL,
    -- Total promotional discount applied
    "product_discount" BIGINT NOT NULL,
    "transport_cost" BIGINT NOT NULL,
    -- Final amount charged: product_cost - product_discount + transport_cost
    "total" BIGINT NOT NULL,
    "note" TEXT,
    -- Miscellaneous order metadata (e.g. promo codes applied, coupon IDs)
    "data" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "order_pkey" PRIMARY KEY ("id")
);

-- Checkout item: starts as an unconfirmed line item (order_id IS NULL) and is
-- linked to an order once the seller confirms it. sku_name is a snapshot of the
-- product name at purchase time. serial_ids records assigned serial numbers for
-- serialized inventory. Item status is inferred: payment_id NOT NULL + order_id
-- IS NULL → paid/pending seller confirmation; date_cancelled NOT NULL → cancelled.
CREATE TABLE IF NOT EXISTS "order"."item" (
    "id" BIGSERIAL NOT NULL,
    -- NULL until the seller confirms and an order is created
    "order_id" UUID,
    "account_id" UUID NOT NULL,
    "seller_id" UUID NOT NULL,
    "sku_id" UUID NOT NULL,
    -- Snapshot of SKU display name at time of purchase (prevents display issues if renamed)
    "sku_name" TEXT NOT NULL,
    "quantity" BIGINT NOT NULL,
    -- Price per unit at time of purchase
    "unit_price" BIGINT NOT NULL,
    -- Amount actually paid after promotions (may differ from unit_price × quantity)
    "paid_amount" BIGINT NOT NULL DEFAULT 0,
    -- Snapshot of the delivery address
    "address" TEXT NOT NULL DEFAULT '',
    "note" TEXT,
    -- Array of assigned serial number IDs from inventory.serial
    "serial_ids" JSONB,
    -- Pay-first: each item references its payment session
    "payment_id" BIGINT,
    -- Selected transport option at checkout time
    "transport_option" TEXT,
    -- Estimated transport cost quoted at checkout
    "transport_cost_estimate" BIGINT NOT NULL DEFAULT 0,
    -- Set when buyer or system cancels an unpaid/unconfirmed item
    "date_cancelled" TIMESTAMPTZ(3),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "item_pkey" PRIMARY KEY ("id")
);

-- Refund request raised by the buyer after a completed order. transport_id is set
-- when the return transport is created (for PickUp/DropOff methods).
-- confirmed_by_id is the seller or admin who approved the refund.
-- item_ids: NULL = refund all items; array = specific items being refunded.
-- amount: NULL = full order total; value = custom partial amount.
CREATE TABLE IF NOT EXISTS "order"."refund" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "order_id" UUID NOT NULL,
    -- Account (seller/admin) that approved or rejected the refund
    "confirmed_by_id" UUID,
    -- Return transport record; NULL until return label is generated
    "transport_id" UUID,
    "method" "order"."refund_method" NOT NULL,
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
    "reason" TEXT NOT NULL,
    -- Return pickup/dropoff address; required for PickUp/DropOff methods
    "address" TEXT,
    -- Array of item IDs being refunded; NULL means full refund
    "item_ids" JSONB,
    -- Partial refund amount in smallest currency unit; NULL means full refund
    "amount" BIGINT,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "refund_pkey" PRIMARY KEY ("id")
);

-- Formal dispute raised against a refund decision (by buyer or seller).
-- issued_by_id is the account that opened the dispute (either party).
-- resolved_by_id is the platform staff who resolved it.
CREATE TABLE IF NOT EXISTS "order"."refund_dispute" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "refund_id" UUID NOT NULL,
    -- The account that raised the dispute (buyer or seller)
    "issued_by_id" UUID NOT NULL,
    "reason" TEXT NOT NULL,
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
    -- Account that resolved the dispute (platform staff)
    "resolved_by_id" UUID,
    -- Free-form note explaining the resolution decision
    "resolution_note" TEXT,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- When resolution was recorded
    "date_resolved" TIMESTAMPTZ(3),
    CONSTRAINT "refund_dispute_pkey" PRIMARY KEY ("id")
);

-- Indexes

-- One cart row per (account, SKU) pair
CREATE UNIQUE INDEX IF NOT EXISTS "cart_item_account_id_sku_id_key" ON "order"."cart_item" ("account_id", "sku_id");
CREATE INDEX IF NOT EXISTS "order_seller_id_idx" ON "order"."order" ("seller_id");
CREATE INDEX IF NOT EXISTS "item_order_id_idx" ON "order"."item" ("order_id");
CREATE INDEX IF NOT EXISTS "item_sku_id_idx" ON "order"."item" ("sku_id");
-- Find items by payment_id (for webhook confirmation)
CREATE INDEX IF NOT EXISTS "idx_item_payment_id" ON "order"."item" ("payment_id") WHERE "payment_id" IS NOT NULL;
-- Seller's pending inbox: paid items awaiting confirmation
CREATE INDEX IF NOT EXISTS "idx_item_seller_pending" ON "order"."item" ("seller_id", "transport_option") WHERE "order_id" IS NULL AND "date_cancelled" IS NULL;
CREATE INDEX IF NOT EXISTS "refund_account_id_idx" ON "order"."refund" ("account_id");
CREATE INDEX IF NOT EXISTS "refund_order_id_idx" ON "order"."refund" ("order_id");
CREATE INDEX IF NOT EXISTS "refund_confirmed_by_id_idx" ON "order"."refund" ("confirmed_by_id");
CREATE INDEX IF NOT EXISTS "refund_transport_id_idx" ON "order"."refund" ("transport_id");
CREATE INDEX IF NOT EXISTS "refund_dispute_refund_id_idx" ON "order"."refund_dispute" ("refund_id");
CREATE INDEX IF NOT EXISTS "refund_dispute_issued_by_id_idx" ON "order"."refund_dispute" ("issued_by_id");
CREATE INDEX IF NOT EXISTS "refund_dispute_resolved_by_id_idx" ON "order"."refund_dispute" ("resolved_by_id");

-- Foreign keys

ALTER TABLE "order"."order"
    ADD CONSTRAINT "order_transport_id_fkey"
    FOREIGN KEY ("transport_id") REFERENCES "order"."transport" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

ALTER TABLE "order"."item"
    ADD CONSTRAINT "item_order_id_fkey"
    FOREIGN KEY ("order_id") REFERENCES "order"."order" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "order"."item"
    ADD CONSTRAINT "item_payment_id_fkey"
    FOREIGN KEY ("payment_id") REFERENCES "order"."payment" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- NO ACTION: keep refund records even if the order is somehow deleted
ALTER TABLE "order"."refund"
    ADD CONSTRAINT "refund_order_id_fkey"
    FOREIGN KEY ("order_id") REFERENCES "order"."order" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;

ALTER TABLE "order"."refund"
    ADD CONSTRAINT "refund_transport_id_fkey"
    FOREIGN KEY ("transport_id") REFERENCES "order"."transport" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;

ALTER TABLE "order"."refund_dispute"
    ADD CONSTRAINT "refund_dispute_refund_id_fkey"
    FOREIGN KEY ("refund_id") REFERENCES "order"."refund" ("id") ON DELETE NO ACTION ON UPDATE CASCADE;
