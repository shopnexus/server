CREATE SCHEMA IF NOT EXISTS "account";

CREATE TYPE "account"."type" AS ENUM ('Customer', 'Vendor');
CREATE TYPE "account"."status" AS ENUM ('Active', 'Suspended');
CREATE TYPE "account"."gender" AS ENUM ('Male', 'Female', 'Other');
CREATE TYPE "account"."address_type" AS ENUM ('Home', 'Work');

CREATE TABLE IF NOT EXISTS "account"."account" (
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

CREATE TABLE IF NOT EXISTS "account"."profile" (
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

CREATE TABLE IF NOT EXISTS "account"."customer" (
    "id" UUID NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "customer_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "account"."vendor" (
    "id" UUID NOT NULL,
    "description" TEXT NOT NULL DEFAULT '',
    CONSTRAINT "vendor_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "account"."income_history" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID NOT NULL,
    "type" VARCHAR(50) NOT NULL,
    "income" BIGINT NOT NULL,
    "current_balance" BIGINT NOT NULL,
    "note" VARCHAR(100),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "income_history_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "account"."notification" (
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

CREATE TABLE IF NOT EXISTS "account"."contact" (
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

CREATE UNIQUE INDEX IF NOT EXISTS "account_phone_key" ON "account"."account" ("phone");
CREATE UNIQUE INDEX IF NOT EXISTS "account_email_key" ON "account"."account" ("email");
CREATE UNIQUE INDEX IF NOT EXISTS "account_username_key" ON "account"."account" ("username");
CREATE UNIQUE INDEX IF NOT EXISTS "profile_avatar_rs_id_key" ON "account"."profile" ("avatar_rs_id");
CREATE UNIQUE INDEX IF NOT EXISTS "profile_default_contact_id_key" ON "account"."profile" ("default_contact_id");
CREATE INDEX IF NOT EXISTS "vendor_id_idx" ON "account"."vendor" ("id");
CREATE INDEX IF NOT EXISTS "income_history_account_id_idx" ON "account"."income_history" ("account_id");
CREATE INDEX IF NOT EXISTS "income_history_type_idx" ON "account"."income_history" ("type");
CREATE INDEX IF NOT EXISTS "income_history_date_created_idx" ON "account"."income_history" ("date_created");
CREATE INDEX IF NOT EXISTS "notification_account_id_idx" ON "account"."notification" ("account_id");
CREATE INDEX IF NOT EXISTS "notification_type_idx" ON "account"."notification" ("type");
CREATE INDEX IF NOT EXISTS "notification_channel_idx" ON "account"."notification" ("channel");
CREATE INDEX IF NOT EXISTS "notification_date_created_idx" ON "account"."notification" ("date_created");
CREATE INDEX IF NOT EXISTS "contact_account_id_idx" ON "account"."contact" ("account_id");

ALTER TABLE "account"."profile"
    ADD CONSTRAINT "profile_default_contact_id_fkey"
    FOREIGN KEY ("default_contact_id") REFERENCES "account"."contact" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

ALTER TABLE "account"."profile"
    ADD CONSTRAINT "profile_id_fkey"
    FOREIGN KEY ("id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."customer"
    ADD CONSTRAINT "customer_id_fkey"
    FOREIGN KEY ("id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."vendor"
    ADD CONSTRAINT "vendor_id_fkey"
    FOREIGN KEY ("id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."income_history"
    ADD CONSTRAINT "income_history_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."vendor" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."notification"
    ADD CONSTRAINT "notification_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE "account"."contact"
    ADD CONSTRAINT "contact_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;