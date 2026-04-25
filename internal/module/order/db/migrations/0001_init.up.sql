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
-- Generic status used by payment, order, refund, and refund_dispute tables
CREATE TYPE "order"."status" AS ENUM ('Pending', 'Processing', 'Success', 'Cancelled', 'Failed');

-- Tables

-- Flat shopping cart: one row per (account, SKU) pair.
CREATE TABLE IF NOT EXISTS "order"."cart_item" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "sku_id" UUID NOT NULL,
    "quantity" BIGINT NOT NULL,

    CONSTRAINT "cart_item_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "cart_item_account_id_sku_id_key" UNIQUE ("account_id", "sku_id")
);

-- Transaction session record.
CREATE TABLE IF NOT EXISTS "order"."transaction" (
    "id" BIGSERIAL NOT NULL,
    "from_id" UUID, -- Account that initiated the transaction (e.g. buyer for payments, seller for payouts, NULL for system like refunds)
    "to_id" UUID, -- Same with from_id but mostly are system (except payouts to sellers, refunds to buyers, etc.)
    "type" TEXT NOT NULL, -- Grouping key (e.g. 'buyer-payment', 'seller-payout', 'buyer-refund', 'confirmation_fee', etc.)
    "status" "order"."status" NOT NULL,
    "note" TEXT NOT NULL, -- System note

    -- Pay by what? If all NULL means use internal wallet
    "payment_option" TEXT, -- Link to common.service_option; If paid by transfer
    "wallet_id" UUID, -- If paid with a stored account.wallet (e.g. debit/credit card, e-wallet)

    -- Audit trail
    -- Cost breakdown and metadata for payment processing (e.g. line items, applied promotions, tax, fees)
    -- And miscellaneous order metadata (e.g. promo codes applied, coupon IDs)
    "data" JSONB NOT NULL,

    -- Capture exchange rate for currency conversion at the payment time
    "amount" BIGINT NOT NULL, -- Either positive or negative
    "from_currency" VARCHAR(3) NOT NULL,
    "to_currency" VARCHAR(3) NOT NULL,
    "exchange_rate" NUMERIC NOT NULL,

    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_paid" TIMESTAMPTZ(3), -- Set when payment is confirmed successful
    "date_expired" TIMESTAMPTZ(3) NOT NULL, -- Payment session expiry; unpaid after this timestamp are voided

    CONSTRAINT "transaction_pkey" PRIMARY KEY ("id")
);

-- Transport/delivery record. option references common.service_option.id
CREATE TABLE IF NOT EXISTS "order"."transport" (
    "id" BIGSERIAL NOT NULL,
    "option" TEXT NOT NULL, -- References common.service_option.id (shipping provider)
    "status" "order"."status" DEFAULT 'Pending',
    "data" JSONB NOT NULL, -- Provider-specific data (tracking number, label URL, webhook events, etc.)
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "transport_pkey" PRIMARY KEY ("id")
);

-- Order created when a seller confirms pending items.
CREATE TABLE IF NOT EXISTS "order"."order" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "buyer_id" UUID NOT NULL,
    "seller_id" UUID NOT NULL, -- Denormalized from order items for easier querying;
    "transport_id" BIGINT NOT NULL,
    "address" TEXT NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Seller confirmation of the order
    "confirmed_by_id" UUID NOT NULL, -- Seller may have many accounts (staff)
    "seller_tx_id" BIGINT NOT NULL, -- Link to the transaction record for confirmation fees (transport cost, etc.)
    "note" TEXT, -- Seller note

    CONSTRAINT "order_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "order_transport_id_fkey" FOREIGN KEY ("transport_id")
        REFERENCES "order"."transport" ("id") ON DELETE NO ACTION ON UPDATE CASCADE,
    CONSTRAINT "order_seller_tx_id_fkey" FOREIGN KEY ("seller_tx_id")
        REFERENCES "order"."transaction" ("id") ON DELETE NO ACTION ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "order_buyer_id_idx" ON "order"."order" ("buyer_id");
CREATE INDEX IF NOT EXISTS "order_seller_id_idx" ON "order"."order" ("seller_id");
CREATE INDEX IF NOT EXISTS "order_transport_id_idx" ON "order"."order" ("transport_id");

-- Checkout item: starts unconfirmed (order_id IS NULL), linked to an order on seller confirmation.
CREATE TABLE IF NOT EXISTS "order"."item" (
    "id" BIGSERIAL NOT NULL,
    "order_id" UUID, -- NULL until the seller confirms
    "account_id" UUID NOT NULL,
    "seller_id" UUID NOT NULL, -- Denormalized from sku->spu->seller
    "sku_id" UUID NOT NULL,
    "spu_id" UUID NOT NULL, -- Snapshot of the SKU's parent SPU at time of purchase; used by review flows to scope comments to product family
    "sku_name" TEXT NOT NULL, -- Snapshot of SKU display name at time of purchase (prevents display issues if renamed)
    "address" TEXT NOT NULL, -- Snapshot of the delivery address
    "note" TEXT, -- Buyer note

    "serial_ids" JSONB, -- Array of assigned serial number IDs from inventory.serial (if serial_required)

    -- PAY-FIRST
    "quantity" BIGINT NOT NULL,
    "transport_option" TEXT NOT NULL,
    "subtotal_amount" BIGINT NOT NULL, -- quantity * unit price. Used for display
    "paid_amount" BIGINT NOT NULL, -- Final paid amount after discounts, taxes, etc. Used for display & refunds
    "payment_tx_id" BIGINT NOT NULL,

    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Cancellation
    "date_cancelled" TIMESTAMPTZ(3), -- Set when buyer or seller cancels the item
    "cancelled_by_id" UUID, -- Account that cancelled the item (buyer or seller, NULL means system)
    "refund_tx_id" BIGINT, -- Updated when an item is cancelled and refunded; link to the refund transaction record

    CONSTRAINT "item_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "item_order_id_fkey" FOREIGN KEY ("order_id")
        REFERENCES "order"."order" ("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "item_payment_tx_id_fkey" FOREIGN KEY ("payment_tx_id")
        REFERENCES "order"."transaction" ("id") ON DELETE NO ACTION ON UPDATE CASCADE,
    CONSTRAINT "item_refund_tx_id_fkey" FOREIGN KEY ("refund_tx_id")
        REFERENCES "order"."transaction" ("id") ON DELETE NO ACTION ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "item_order_id_idx" ON "order"."item" ("order_id");
CREATE INDEX IF NOT EXISTS "item_sku_id_idx" ON "order"."item" ("sku_id");
-- Seller's pending inbox: paid items awaiting confirmation
CREATE INDEX IF NOT EXISTS "idx_item_seller_pending" ON "order"."item" ("seller_id", "transport_option") WHERE "order_id" IS NULL AND "date_cancelled" IS NULL;

-- Refund request raised by the buyer after a completed order. Two stages of approval: seller confirmation before transportation, then final approval after transportation.
CREATE TABLE IF NOT EXISTS "order"."refund" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "order_item_id" BIGINT NOT NULL,
    "transport_id" BIGINT NOT NULL,
    "method" "order"."refund_method" NOT NULL,
    "reason" TEXT NOT NULL,
    "address" TEXT, -- required for PickUp method, NULL for DropOff
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    "status" "order"."status" NOT NULL DEFAULT 'Pending', -- Pending →  Processing (Accepted - Before transportation) → Processing (Approved - After transportation, inspect) → Success/Failed

    -- Stage 1: Seller accepts refund request (before transportation)
    "accepted_by_id"  UUID,
    "date_accepted"   TIMESTAMPTZ(3),
    "rejection_note" TEXT,

    -- Stage 2: Seller inspects item and approves refund payment (after transportation)
    "approved_by_id"  UUID,
    "date_approved"   TIMESTAMPTZ(3),
    "refund_tx_id"    BIGINT,

    CONSTRAINT "refund_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "refund_order_item_id_fkey" FOREIGN KEY ("order_item_id")
        REFERENCES "order"."item" ("id") ON DELETE NO ACTION ON UPDATE CASCADE,
    CONSTRAINT "refund_transport_id_fkey" FOREIGN KEY ("transport_id")
        REFERENCES "order"."transport" ("id") ON DELETE NO ACTION ON UPDATE CASCADE,
    CONSTRAINT "refund_refund_tx_id_fkey" FOREIGN KEY ("refund_tx_id")
        REFERENCES "order"."transaction" ("id") ON DELETE NO ACTION ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "refund_account_id_idx" ON "order"."refund" ("account_id");
CREATE INDEX IF NOT EXISTS "refund_order_item_id_idx" ON "order"."refund" ("order_item_id");
CREATE INDEX IF NOT EXISTS "refund_accepted_by_id_idx" ON "order"."refund" ("accepted_by_id");
CREATE INDEX IF NOT EXISTS "refund_transport_id_idx" ON "order"."refund" ("transport_id");
CREATE INDEX IF NOT EXISTS "refund_approved_by_id_idx" ON "order"."refund" ("approved_by_id");

-- Formal dispute raised against a refund decision by the any account (buyer, seller) for platform review and resolution.
CREATE TABLE IF NOT EXISTS "order"."refund_dispute" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL, -- The account that raised the dispute
    "refund_id" UUID NOT NULL,
    "reason" TEXT NOT NULL,
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
    "note" TEXT NOT NULL, -- Free-form note explaining the resolution decision
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Resolved by platform staff after reviewing the dispute
    "resolved_by_id" UUID, -- Account that resolved the dispute (platform staff)
    "date_resolved" TIMESTAMPTZ(3), -- When resolution was recorded

    CONSTRAINT "refund_dispute_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "refund_dispute_refund_id_fkey" FOREIGN KEY ("refund_id")
        REFERENCES "order"."refund" ("id") ON DELETE NO ACTION ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "refund_dispute_refund_id_idx" ON "order"."refund_dispute" ("refund_id");
CREATE INDEX IF NOT EXISTS "refund_dispute_account_id_idx" ON "order"."refund_dispute" ("account_id");
CREATE INDEX IF NOT EXISTS "refund_dispute_resolved_by_id_idx" ON "order"."refund_dispute" ("resolved_by_id");
