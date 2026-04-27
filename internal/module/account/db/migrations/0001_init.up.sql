-- =============================================
-- Module: Account
-- Schema: account
-- Description: User accounts, profiles, contacts, notifications,
--              payment methods, favorites, income history, and wallet.
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

    CONSTRAINT "account_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "account_phone_key" UNIQUE ("phone"),
    CONSTRAINT "account_email_key" UNIQUE ("email"),
    CONSTRAINT "account_username_key" UNIQUE ("username")
);

-- Extended public profile details; 1-1 with account via shared PK.
CREATE TABLE IF NOT EXISTS "account"."profile" (
    "id" UUID NOT NULL,
    "gender" "account"."gender",
    "name" VARCHAR(100) NOT NULL,
    "description" TEXT NOT NULL DEFAULT '',
    "date_of_birth" TIMESTAMP(3),
    "avatar_rs_id" UUID,
    "email_verified" BOOLEAN NOT NULL DEFAULT false,
    "phone_verified" BOOLEAN NOT NULL DEFAULT false,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    "balance" BIGINT NOT NULL DEFAULT 0, -- Internal money
    "country" VARCHAR(2) NOT NULL, -- Explicit for money currency, can only updated when balance is zero

    -- Default
    "default_contact_id" UUID,
    "default_wallet_id" UUID,

    CONSTRAINT "profile_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "profile_avatar_rs_id_key" UNIQUE ("avatar_rs_id"),
    CONSTRAINT "profile_country_format" CHECK ("country" ~ '^[A-Z]{2}$'),
    CONSTRAINT "profile_balance_non_negative" CHECK ("balance" >= 0),

    -- profile shares the same PK as account (1-1 relationship)
    CONSTRAINT "profile_id_fkey" FOREIGN KEY ("id")
        REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);

-- Saved addresses and contact details used for shipping and billing.
CREATE TABLE IF NOT EXISTS "account"."contact" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "full_name" VARCHAR(100) NOT NULL,
    "phone" VARCHAR(30) NOT NULL,
    "phone_verified" BOOLEAN NOT NULL DEFAULT false,
    "address_type" "account"."address_type" NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Geocoding fields for the address; can be used for distance-based shipping promos, nearest store locator, etc.
    "address" VARCHAR(255) NOT NULL,
    "latitude" DOUBLE PRECISION NOT NULL, -- lat + long must be nearby the text address
    "longitude" DOUBLE PRECISION NOT NULL,

    CONSTRAINT "contact_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "contact_account_id_fkey" FOREIGN KEY ("account_id")
        REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
-- At most one default contact per account (partial unique: stays out-of-line)
CREATE INDEX IF NOT EXISTS "contact_account_id_idx" ON "account"."contact" ("account_id");

-- In-app notifications delivered via various channels (push, email, SMS, etc.).
CREATE TABLE IF NOT EXISTS "account"."notification" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "type" VARCHAR(50) NOT NULL, -- Notification category (e.g. 'OrderUpdate', 'Promotion')
    "channel" VARCHAR(50) NOT NULL, -- Delivery channel (e.g. 'Push', 'Email', 'SMS')
    "title" VARCHAR(200) NOT NULL,
    "is_read" BOOLEAN NOT NULL DEFAULT false,
    "content" TEXT NOT NULL,
    "metadata" JSONB, -- Additional structured payload for deep-links, images, etc.
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_sent" TIMESTAMPTZ(3),
    "date_scheduled" TIMESTAMPTZ(3),

    CONSTRAINT "notification_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "notification_account_id_fkey" FOREIGN KEY ("account_id")
        REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "notification_account_id_idx" ON "account"."notification" ("account_id");
CREATE INDEX IF NOT EXISTS "notification_type_idx" ON "account"."notification" ("type");
CREATE INDEX IF NOT EXISTS "notification_channel_idx" ON "account"."notification" ("channel");
CREATE INDEX IF NOT EXISTS "notification_date_created_idx" ON "account"."notification" ("date_created");

-- Wishlist / saved products. spu_id references catalog.product_spu (no FK enforced).
CREATE TABLE IF NOT EXISTS "account"."favorite" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "spu_id" UUID NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "favorite_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "favorite_account_id_spu_id_key" UNIQUE ("account_id", "spu_id"),

    CONSTRAINT "favorite_account_id_fkey" FOREIGN KEY ("account_id")
        REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "favorite_spu_id_idx" ON "account"."favorite" ("spu_id");

-- Stored payment wallet (credit card tokens, e-wallet refs, bank accounts, etc.).
CREATE TABLE IF NOT EXISTS "account"."wallet" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "option" VARCHAR(100) NOT NULL, -- References common.service_option (e.g. 'vnpay_qr', 'sepay_bank_transfer', 'card_stripe')
    "label" VARCHAR(100) NOT NULL, -- Human-readable label shown in the UI (e.g. 'Visa ending in 4242')
    "data" JSONB NOT NULL, -- Provider-specific token/reference data (no raw card numbers stored here)
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "wallet_pkey" PRIMARY KEY ("id"),

    CONSTRAINT "wallet_account_id_fkey" FOREIGN KEY ("account_id")
        REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "wallet_account_id_idx" ON "account"."wallet" ("account_id");

-- Cross-table FKs from account.account, deferred to here because contact
-- and wallet also FK back to account (circular).
ALTER TABLE "account"."account"
    ADD CONSTRAINT "profile_default_contact_id_fkey" FOREIGN KEY ("default_contact_id")
        REFERENCES "account"."contact" ("id") ON DELETE SET NULL ON UPDATE CASCADE,
    ADD CONSTRAINT "profile_default_wallet_id_fkey" FOREIGN KEY ("default_wallet_id")
        REFERENCES "account"."wallet" ("id") ON DELETE SET NULL ON UPDATE CASCADE;
