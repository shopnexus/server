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

-- Payment intent: one logical money flow (checkout, refund, payout, fee).
-- Mutable status; has 0..N transaction rows below for split-tender support.
CREATE TABLE IF NOT EXISTS "order"."payment_session" (
    "id" UUID NOT NULL, -- App-allocated UUID; equals the Restate workflow ID for sessions backed by a workflow
    "kind" TEXT NOT NULL, -- 'buyer-checkout' | 'seller-confirmation-fee' | 'seller-payout'; enum defined in app layer
    "status" "order"."status" NOT NULL,
    "from_id" UUID, -- Account initiating (buyer, seller, NULL = system)
    "to_id" UUID, -- Counterparty (buyer, seller, NULL = system)
    "note" TEXT NOT NULL,

    "currency" VARCHAR(3) NOT NULL,
    "total_amount" BIGINT NOT NULL, -- Expected total in buyer-facing currency

    -- Checkout context shared across rails: cost breakdown, line items snapshot,
    -- applied promotions, gateway URLs per rail, provider metadata
    "data" JSONB NOT NULL,

    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_paid" TIMESTAMPTZ(3), -- Set when session reaches Success
    "date_expired" TIMESTAMPTZ(3) NOT NULL, -- Pending sessions auto-void after this timestamp

    CONSTRAINT "payment_session_pkey" PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "payment_session_kind_idx" ON "order"."payment_session" ("kind");
CREATE INDEX IF NOT EXISTS "payment_session_from_id_idx" ON "order"."payment_session" ("from_id");
CREATE INDEX IF NOT EXISTS "payment_session_status_pending_idx" ON "order"."payment_session" ("status") WHERE "status" IN ('Pending', 'Processing');

-- Append-only ledger leg: one row per rail movement (wallet debit, card charge,
-- refund leg). Status transitions Pending -> Success/Failed only; Success is terminal.
-- Reversals are NEW rows with negative amount + reverses_id pointing to the original.
CREATE TABLE IF NOT EXISTS "order"."transaction" (
    "id" BIGSERIAL NOT NULL,
    "session_id" UUID NOT NULL,
    "status" "order"."status" NOT NULL,
    "note" TEXT NOT NULL,
    "error" TEXT,

    -- Concrete rail used. Both NULL = internal wallet (system credit / debit)
    "payment_option" TEXT, -- common.service_option if paid by transfer to system
    "wallet_id" UUID, -- account.wallet.id if paid via stored card / e-wallet

    -- Rail-specific payload: gateway request/response, webhook payload, processor IDs
    "data" JSONB NOT NULL,

    -- Signed: positive = original charge; negative = reversal (refund leg).
    -- Per-rail currency because split-tender may mix currencies (e.g. wallet VND + card USD).
    "amount" BIGINT NOT NULL,
    "from_currency" VARCHAR(3) NOT NULL,
    "to_currency" VARCHAR(3) NOT NULL,
    "exchange_rate" NUMERIC NOT NULL,

    -- Self-FK to the original charge this row reverses; NULL on originals.
    "reverses_id" BIGINT,

    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_settled" TIMESTAMPTZ(3), -- Set when status reaches Success
    "date_expired" TIMESTAMPTZ(3), -- Gateway URL expiry; NULL for internal wallet rails

    CONSTRAINT "transaction_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "transaction_sign_matches_reverses_chk" CHECK ((amount > 0 AND reverses_id IS NULL) OR (amount < 0 AND reverses_id IS NOT NULL)),
    CONSTRAINT "transaction_no_self_reverse_chk" CHECK (reverses_id IS NULL OR reverses_id != id),

    CONSTRAINT "transaction_session_id_fkey" FOREIGN KEY ("session_id")
        REFERENCES "order"."payment_session" ("id") ON DELETE NO ACTION ON UPDATE CASCADE,
    CONSTRAINT "transaction_reverses_id_fkey" FOREIGN KEY ("reverses_id")
        REFERENCES "order"."transaction" ("id") ON DELETE NO ACTION ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "transaction_session_id_idx" ON "order"."transaction" ("session_id");
CREATE UNIQUE INDEX IF NOT EXISTS "transaction_reverses_id_unique" ON "order"."transaction" ("reverses_id") WHERE "reverses_id" IS NOT NULL;

-- Settled-only view for analytics / ledger queries; hides Pending/Failed rows.
-- Convention: revenue, refund total, dashboard SQL reads from this view, not the table.
CREATE OR REPLACE VIEW "order"."transaction_settled" AS
    SELECT * FROM "order"."transaction" WHERE "status" = 'Success';

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
    "confirm_session_id" UUID NOT NULL, -- Seller confirmation fee session (kind='seller-confirmation-fee')
    "note" TEXT, -- Seller note

    CONSTRAINT "order_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "order_transport_id_fkey" FOREIGN KEY ("transport_id")
        REFERENCES "order"."transport" ("id") ON DELETE NO ACTION ON UPDATE CASCADE,
    CONSTRAINT "order_confirm_session_id_fkey" FOREIGN KEY ("confirm_session_id")
        REFERENCES "order"."payment_session" ("id") ON DELETE NO ACTION ON UPDATE CASCADE
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
    "total_amount" BIGINT NOT NULL, -- Final paid amount after discounts, taxes, etc. Used for display & refunds
    "payment_session_id" UUID NOT NULL,

    -- Cancellation
    "date_cancelled" TIMESTAMPTZ(3), -- Set when buyer or seller cancels the item
    "cancelled_by_id" UUID, -- Account that cancelled the item (buyer or seller, NULL means system)

    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "item_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "item_order_id_fkey" FOREIGN KEY ("order_id")
        REFERENCES "order"."order" ("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "item_payment_session_id_fkey" FOREIGN KEY ("payment_session_id")
        REFERENCES "order"."payment_session" ("id") ON DELETE NO ACTION ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "item_order_id_idx" ON "order"."item" ("order_id");
CREATE INDEX IF NOT EXISTS "item_sku_id_idx" ON "order"."item" ("sku_id");
-- Seller's pending inbox: paid items awaiting confirmation
CREATE INDEX IF NOT EXISTS "idx_item_seller_pending" ON "order"."item" ("seller_id", "transport_option") WHERE "order_id" IS NULL AND "date_cancelled" IS NULL;

-- Refund request raised by the buyer after a completed order. Two stages of approval: seller confirmation before transportation, then final approval after transportation.
CREATE TABLE IF NOT EXISTS "order"."refund" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "order_id" UUID NOT NULL,
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
    "approved_by_id" UUID,
    "date_approved"  TIMESTAMPTZ(3),
    "refund_tx_id"   BIGINT, -- Negative-amount tx (in item's payment_session) representing the refund credit; convention: single rail (internal wallet)

    CONSTRAINT "refund_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "refund_order_id_fkey" FOREIGN KEY ("order_id")
        REFERENCES "order"."order" ("id") ON DELETE NO ACTION ON UPDATE CASCADE,
    CONSTRAINT "refund_transport_id_fkey" FOREIGN KEY ("transport_id")
        REFERENCES "order"."transport" ("id") ON DELETE NO ACTION ON UPDATE CASCADE,
    CONSTRAINT "refund_refund_tx_id_fkey" FOREIGN KEY ("refund_tx_id")
        REFERENCES "order"."transaction" ("id") ON DELETE NO ACTION ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "refund_account_id_idx" ON "order"."refund" ("account_id");
CREATE INDEX IF NOT EXISTS "refund_order_id_idx" ON "order"."refund" ("order_id");
CREATE INDEX IF NOT EXISTS "refund_accepted_by_id_idx" ON "order"."refund" ("accepted_by_id");
CREATE INDEX IF NOT EXISTS "refund_transport_id_idx" ON "order"."refund" ("transport_id");
CREATE INDEX IF NOT EXISTS "refund_approved_by_id_idx" ON "order"."refund" ("approved_by_id");
CREATE UNIQUE INDEX IF NOT EXISTS "refund_one_active_per_order"
    ON "order"."refund" ("order_id")
    WHERE "status" IN ('Pending', 'Processing');

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
