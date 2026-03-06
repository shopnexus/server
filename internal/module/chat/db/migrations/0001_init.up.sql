CREATE SCHEMA IF NOT EXISTS "chat";

CREATE TYPE "chat"."message_type" AS ENUM ('Text', 'Image', 'System');
CREATE TYPE "chat"."message_status" AS ENUM ('Sent', 'Delivered', 'Read');

CREATE TABLE IF NOT EXISTS "chat"."conversation" (
    "id"              UUID NOT NULL DEFAULT gen_random_uuid(),
    "customer_id"     UUID NOT NULL,
    "vendor_id"       UUID NOT NULL,
    "last_message_at" TIMESTAMPTZ(3),
    "date_created"    TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "conversation_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "conversation_customer_vendor_key" ON "chat"."conversation" ("customer_id", "vendor_id");
CREATE INDEX IF NOT EXISTS "conversation_customer_id_idx" ON "chat"."conversation" ("customer_id");
CREATE INDEX IF NOT EXISTS "conversation_vendor_id_idx" ON "chat"."conversation" ("vendor_id");
CREATE INDEX IF NOT EXISTS "conversation_last_message_at_idx" ON "chat"."conversation" ("last_message_at" DESC);

ALTER TABLE "chat"."conversation"
    ADD CONSTRAINT "conversation_customer_id_fkey"
    FOREIGN KEY ("customer_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
ALTER TABLE "chat"."conversation"
    ADD CONSTRAINT "conversation_vendor_id_fkey"
    FOREIGN KEY ("vendor_id") REFERENCES "account"."vendor" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

CREATE TABLE IF NOT EXISTS "chat"."message" (
    "id"              BIGSERIAL NOT NULL,
    "conversation_id" UUID NOT NULL,
    "sender_id"       UUID NOT NULL,
    "type"            "chat"."message_type" NOT NULL DEFAULT 'Text',
    "content"         TEXT NOT NULL,
    "status"          "chat"."message_status" NOT NULL DEFAULT 'Sent',
    "metadata"        JSONB,
    "date_created"    TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "message_pkey" PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "message_conversation_id_idx" ON "chat"."message" ("conversation_id", "date_created" DESC);
CREATE INDEX IF NOT EXISTS "message_sender_id_idx" ON "chat"."message" ("sender_id");

ALTER TABLE "chat"."message"
    ADD CONSTRAINT "message_conversation_id_fkey"
    FOREIGN KEY ("conversation_id") REFERENCES "chat"."conversation" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
ALTER TABLE "chat"."message"
    ADD CONSTRAINT "message_sender_id_fkey"
    FOREIGN KEY ("sender_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
