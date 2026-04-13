-- =============================================
-- Module: Account
-- Schema: account
-- Description: User accounts, profiles, contacts, notifications,
--              payment methods, favorites, and income history.
--              Any account can act as both buyer and seller.
-- =============================================

CREATE SCHEMA IF NOT EXISTS "account";

-- Enums

-- Account lifecycle state
CREATE TYPE "account"."status" AS ENUM ('Active', 'Suspended');
-- Self-reported gender for the profile
CREATE TYPE "account"."gender" AS ENUM ('Male', 'Female', 'Other');
-- Address classification for contacts
CREATE TYPE "account"."address_type" AS ENUM ('Home', 'Work');

-- Tables

-- Core identity record; phone/email/username are all optional but at least one is
-- typically required for login. "number" is a human-readable sequential ID.
CREATE TABLE IF NOT EXISTS "account"."account" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "number" BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY,
    "status" "account"."status" NOT NULL DEFAULT 'Active',
    "phone" VARCHAR(50),
    "email" VARCHAR(255),
    "username" VARCHAR(100),
    "password" VARCHAR(255),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "account_pkey" PRIMARY KEY ("id")
);

-- Extended public profile details; 1-1 with account via shared PK.
-- avatar_rs_id references common.resource; default_contact_id points to the
-- preferred shipping/billing contact for this account.
CREATE TABLE IF NOT EXISTS "account"."profile" (
    "id" UUID NOT NULL,
    "gender" "account"."gender",
    "name" VARCHAR(100),
    "description" TEXT NOT NULL DEFAULT '',
    "date_of_birth" TIMESTAMP(3),
    "avatar_rs_id" UUID,
    -- FK to common.resource (enforced externally)
    "email_verified" BOOLEAN NOT NULL DEFAULT false,
    "phone_verified" BOOLEAN NOT NULL DEFAULT false,
    "default_contact_id" UUID,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "profile_pkey" PRIMARY KEY ("id")
);

-- Append-only ledger of balance changes (sales proceeds, refunds, withdrawals, etc.).
-- current_balance is a snapshot at the time of the event, not recomputed from history.
CREATE TABLE IF NOT EXISTS "account"."income_history" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    -- Event kind (e.g. 'SaleCredit', 'Withdrawal', 'RefundDebit')
    "type" VARCHAR(50) NOT NULL,
    -- Delta applied in this event (may be negative for debits)
    "income" BIGINT NOT NULL,
    -- Running balance after this event
    "current_balance" BIGINT NOT NULL,
    "note" VARCHAR(100),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "income_history_pkey" PRIMARY KEY ("id")
);

-- In-app notifications delivered via various channels (push, email, SMS, etc.).
-- date_scheduled allows future-dated delivery; date_sent is set once dispatched.
CREATE TABLE IF NOT EXISTS "account"."notification" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    -- Notification category (e.g. 'OrderUpdate', 'Promotion')
    "type" VARCHAR(50) NOT NULL,
    -- Delivery channel (e.g. 'Push', 'Email', 'SMS')
    "channel" VARCHAR(50) NOT NULL,
    "title" VARCHAR(200) NOT NULL,
    "is_read" BOOLEAN NOT NULL DEFAULT false,
    "content" TEXT NOT NULL,
    -- Additional structured payload for deep-links, images, etc.
    "metadata" JSONB,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_sent" TIMESTAMPTZ(3),
    "date_scheduled" TIMESTAMPTZ(3),
    CONSTRAINT "notification_pkey" PRIMARY KEY ("id")
);

-- Saved addresses and contact details used for shipping and billing.
-- One contact per account can be marked as default via profile.default_contact_id.
CREATE TABLE IF NOT EXISTS "account"."contact" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "full_name" VARCHAR(100) NOT NULL,
    "phone" VARCHAR(30) NOT NULL,
    "phone_verified" BOOLEAN NOT NULL DEFAULT false,
    "address" VARCHAR(255) NOT NULL,
    "address_type" "account"."address_type" NOT NULL,
    -- Optional geocoordinates for distance-based shipping calculations
    "latitude" DOUBLE PRECISION,
    "longitude" DOUBLE PRECISION,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "contact_pkey" PRIMARY KEY ("id")
);

-- Wishlist / saved products. spu_id references catalog.product_spu (no FK enforced).
-- Unique on (account_id, spu_id) to prevent duplicate saves.
CREATE TABLE IF NOT EXISTS "account"."favorite" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "spu_id" UUID NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "favorite_pkey" PRIMARY KEY ("id")
);

-- Stored payment instruments (credit card tokens, e-wallet refs, bank accounts, etc.).
-- Actual sensitive data is kept in the provider; "data" holds provider-specific metadata.
-- Partial unique index ensures at most one default per account.
CREATE TABLE IF NOT EXISTS "account"."payment_method" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    -- References common.service_option (e.g. 'vnpay_qr', 'sepay_bank_transfer', 'card_stripe')
    "service_option_id" VARCHAR(100) NOT NULL,
    -- Human-readable label shown in the UI (e.g. 'Visa ending in 4242')
    "label" VARCHAR(100) NOT NULL,
    -- Provider-specific token/reference data (no raw card numbers stored here)
    "data" JSONB NOT NULL,
    "is_default" BOOLEAN NOT NULL DEFAULT false,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "payment_method_pkey" PRIMARY KEY ("id")
);

-- Indexes

CREATE UNIQUE INDEX IF NOT EXISTS "account_phone_key" ON "account"."account" ("phone");
CREATE UNIQUE INDEX IF NOT EXISTS "account_email_key" ON "account"."account" ("email");
CREATE UNIQUE INDEX IF NOT EXISTS "account_username_key" ON "account"."account" ("username");
CREATE UNIQUE INDEX IF NOT EXISTS "profile_avatar_rs_id_key" ON "account"."profile" ("avatar_rs_id");
CREATE UNIQUE INDEX IF NOT EXISTS "profile_default_contact_id_key" ON "account"."profile" ("default_contact_id");
CREATE INDEX IF NOT EXISTS "income_history_account_id_idx" ON "account"."income_history" ("account_id");
CREATE INDEX IF NOT EXISTS "income_history_type_idx" ON "account"."income_history" ("type");
CREATE INDEX IF NOT EXISTS "income_history_date_created_idx" ON "account"."income_history" ("date_created");
CREATE INDEX IF NOT EXISTS "notification_account_id_idx" ON "account"."notification" ("account_id");
CREATE INDEX IF NOT EXISTS "notification_type_idx" ON "account"."notification" ("type");
CREATE INDEX IF NOT EXISTS "notification_channel_idx" ON "account"."notification" ("channel");
CREATE INDEX IF NOT EXISTS "notification_date_created_idx" ON "account"."notification" ("date_created");
CREATE INDEX IF NOT EXISTS "contact_account_id_idx" ON "account"."contact" ("account_id");
CREATE UNIQUE INDEX IF NOT EXISTS "favorite_account_id_spu_id_key" ON "account"."favorite" ("account_id", "spu_id");
CREATE INDEX IF NOT EXISTS "favorite_spu_id_idx" ON "account"."favorite" ("spu_id");
CREATE INDEX IF NOT EXISTS "payment_method_account_id_idx" ON "account"."payment_method" ("account_id");
-- At most one default payment method per account
CREATE UNIQUE INDEX IF NOT EXISTS "payment_method_account_default_key"
    ON "account"."payment_method" ("account_id") WHERE "is_default" = true;

-- Foreign keys

ALTER TABLE "account"."profile"
    ADD CONSTRAINT "profile_default_contact_id_fkey"
    FOREIGN KEY ("default_contact_id") REFERENCES "account"."contact" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- profile shares the same PK as account (1-1 relationship)
ALTER TABLE "account"."profile"
    ADD CONSTRAINT "profile_id_fkey"
    FOREIGN KEY ("id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."income_history"
    ADD CONSTRAINT "income_history_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."notification"
    ADD CONSTRAINT "notification_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."contact"
    ADD CONSTRAINT "contact_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."favorite"
    ADD CONSTRAINT "favorite_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."payment_method"
    ADD CONSTRAINT "payment_method_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;