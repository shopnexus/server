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

-- Wallet transaction type
CREATE TYPE "account"."wallet_transaction_type" AS ENUM ('Refund', 'Payment', 'TopUp');

-- Tables
-- Core identity record; phone/email/username are all optional but at least one is
-- typically required for login. "number" is a human-readable sequential ID.
CREATE TABLE
  IF NOT EXISTS "account"."account" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid (),
    "number" BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY,
    "status" "account"."status" NOT NULL DEFAULT 'Active',
    "phone" VARCHAR(50),
    "email" VARCHAR(255),
    "username" VARCHAR(100),
    "password" VARCHAR(255),
    "date_created" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "account_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "account_phone_key" UNIQUE ("phone"),
    CONSTRAINT "account_email_key" UNIQUE ("email"),
    CONSTRAINT "account_username_key" UNIQUE ("username")
  );

-- Saved addresses and contact details used for shipping and billing.
-- Declared before profile so profile.default_contact_id can reference it inline.
CREATE TABLE
  IF NOT EXISTS "account"."contact" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid (),
    "account_id" UUID NOT NULL,
    "full_name" VARCHAR(100) NOT NULL,
    "phone" VARCHAR(30) NOT NULL,
    "phone_verified" BOOLEAN NOT NULL DEFAULT false,
    "address" VARCHAR(255) NOT NULL,
    "address_type" "account"."address_type" NOT NULL,
    -- Optional geocoordinates for distance-based shipping calculations
    "latitude" DOUBLE PRECISION,
    "longitude" DOUBLE PRECISION,
    "date_created" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "contact_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "contact_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
  );

CREATE INDEX IF NOT EXISTS "contact_account_id_idx" ON "account"."contact" ("account_id");

-- Extended public profile details; 1-1 with account via shared PK.
CREATE TABLE
  IF NOT EXISTS "account"."profile" (
    "id" UUID NOT NULL,
    "gender" "account"."gender",
    "name" VARCHAR(100),
    "description" TEXT NOT NULL DEFAULT '',
    "date_of_birth" TIMESTAMP(3),
    "avatar_rs_id" UUID,
    "email_verified" BOOLEAN NOT NULL DEFAULT false,
    "phone_verified" BOOLEAN NOT NULL DEFAULT false,
    "country" VARCHAR(2) NOT NULL DEFAULT 'VN',
    "default_contact_id" UUID,
    "date_created" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- User preferences: preferred_currency (ISO 4217) and future prefs.
    "settings" JSONB NOT NULL DEFAULT '{"preferred_currency": "VND"}',
    CONSTRAINT "profile_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "profile_avatar_rs_id_key" UNIQUE ("avatar_rs_id"),
    CONSTRAINT "profile_default_contact_id_key" UNIQUE ("default_contact_id"),
    CONSTRAINT "profile_country_format" CHECK ("country" ~ '^[A-Z]{2}$'),
    -- profile shares the same PK as account (1-1 relationship)
    CONSTRAINT "profile_id_fkey" FOREIGN KEY ("id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "profile_default_contact_id_fkey" FOREIGN KEY ("default_contact_id") REFERENCES "account"."contact" ("id") ON DELETE SET NULL ON UPDATE CASCADE
  );

-- Append-only ledger of balance changes (sales proceeds, refunds, withdrawals, etc.).
CREATE TABLE
  IF NOT EXISTS "account"."income_history" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    -- Event kind (e.g. 'SaleCredit', 'Withdrawal', 'RefundDebit')
    "type" VARCHAR(50) NOT NULL,
    -- Delta applied in this event (may be negative for debits)
    "income" BIGINT NOT NULL,
    -- Running balance after this event
    "current_balance" BIGINT NOT NULL,
    "note" VARCHAR(100),
    "date_created" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "income_history_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "income_history_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
  );

CREATE INDEX IF NOT EXISTS "income_history_account_id_idx" ON "account"."income_history" ("account_id");
CREATE INDEX IF NOT EXISTS "income_history_type_idx" ON "account"."income_history" ("type");
CREATE INDEX IF NOT EXISTS "income_history_date_created_idx" ON "account"."income_history" ("date_created");

-- In-app notifications delivered via various channels (push, email, SMS, etc.).
CREATE TABLE
  IF NOT EXISTS "account"."notification" (
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
    "date_created" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_sent" TIMESTAMPTZ (3),
    "date_scheduled" TIMESTAMPTZ (3),
    CONSTRAINT "notification_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "notification_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
  );

CREATE INDEX IF NOT EXISTS "notification_account_id_idx" ON "account"."notification" ("account_id");
CREATE INDEX IF NOT EXISTS "notification_type_idx" ON "account"."notification" ("type");
CREATE INDEX IF NOT EXISTS "notification_channel_idx" ON "account"."notification" ("channel");
CREATE INDEX IF NOT EXISTS "notification_date_created_idx" ON "account"."notification" ("date_created");

-- Wishlist / saved products. spu_id references catalog.product_spu (no FK enforced).
CREATE TABLE
  IF NOT EXISTS "account"."favorite" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "spu_id" UUID NOT NULL,
    "date_created" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "favorite_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "favorite_account_id_spu_id_key" UNIQUE ("account_id", "spu_id"),
    CONSTRAINT "favorite_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
  );

CREATE INDEX IF NOT EXISTS "favorite_spu_id_idx" ON "account"."favorite" ("spu_id");

-- Stored payment instruments (credit card tokens, e-wallet refs, bank accounts, etc.).
CREATE TABLE
  IF NOT EXISTS "account"."payment_method" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid (),
    "account_id" UUID NOT NULL,
    -- References common.service_option (e.g. 'vnpay_qr', 'sepay_bank_transfer', 'card_stripe')
    "service_option_id" VARCHAR(100) NOT NULL,
    -- Human-readable label shown in the UI (e.g. 'Visa ending in 4242')
    "label" VARCHAR(100) NOT NULL,
    -- Provider-specific token/reference data (no raw card numbers stored here)
    "data" JSONB NOT NULL,
    "is_default" BOOLEAN NOT NULL DEFAULT false,
    "date_created" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ (3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "payment_method_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "payment_method_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
  );

CREATE INDEX IF NOT EXISTS "payment_method_account_id_idx" ON "account"."payment_method" ("account_id");

-- At most one default payment method per account (partial unique: stays out-of-line)
CREATE UNIQUE INDEX IF NOT EXISTS "payment_method_account_default_key" ON "account"."payment_method" ("account_id")
WHERE
  "is_default" = true;

-- Buyer wallet for balance-based payments (top-up, refund credits, etc.).
CREATE TABLE
  IF NOT EXISTS "account"."wallet" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "balance" BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT "wallet_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "wallet_account_id_key" UNIQUE ("account_id"),
    CONSTRAINT "wallet_balance_non_negative" CHECK ("balance" >= 0),
    CONSTRAINT "wallet_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE
  );

-- Append-only ledger of wallet balance changes.
CREATE TABLE
  IF NOT EXISTS "account"."wallet_transaction" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "type" "account"."wallet_transaction_type" NOT NULL,
    "amount" BIGINT NOT NULL,
    "reference_id" TEXT,
    "note" TEXT,
    "date_created" TIMESTAMPTZ (3) NOT NULL DEFAULT now (),
    CONSTRAINT "wallet_transaction_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "wallet_transaction_account_id_fkey" FOREIGN KEY ("account_id") REFERENCES "account"."wallet" ("account_id") ON DELETE CASCADE ON UPDATE CASCADE
  );

CREATE INDEX IF NOT EXISTS "idx_wallet_tx_account" ON "account"."wallet_transaction" ("account_id", "date_created" DESC);