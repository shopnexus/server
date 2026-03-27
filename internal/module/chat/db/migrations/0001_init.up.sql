-- =============================================
-- Module: Chat
-- Schema: chat
-- Description: Real-time messaging between buyer and seller accounts.
--              Each conversation is a 1-1 thread; messages support text,
--              images, and system-generated events.
-- Note: conversation still uses legacy customer_id / vendor_id column names
--       from before the unified account model; both reference account.account.
-- =============================================

CREATE SCHEMA IF NOT EXISTS "chat";

-- Enums

-- Content type of a chat message
CREATE TYPE "chat"."message_type" AS ENUM ('Text', 'Image', 'System');
-- Delivery state of a message (server-side tracking)
CREATE TYPE "chat"."message_status" AS ENUM ('Sent', 'Delivered', 'Read');

-- Tables

-- A 1-1 conversation thread between two accounts (buyer and seller roles).
-- last_message_at is denormalized for efficient inbox sorting without aggregation.
CREATE TABLE IF NOT EXISTS "chat"."conversation" (
    "id"              UUID NOT NULL DEFAULT gen_random_uuid(),
    -- The account acting as buyer in this conversation
    "customer_id"     UUID NOT NULL,
    -- The account acting as seller in this conversation
    "vendor_id"       UUID NOT NULL,
    -- Denormalized timestamp of the most recent message for inbox ordering
    "last_message_at" TIMESTAMPTZ(3),
    "date_created"    TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "conversation_pkey" PRIMARY KEY ("id")
);

-- Unique pair ensures only one conversation thread exists between any two accounts
CREATE UNIQUE INDEX IF NOT EXISTS "conversation_customer_vendor_key" ON "chat"."conversation" ("customer_id", "vendor_id");
CREATE INDEX IF NOT EXISTS "conversation_customer_id_idx" ON "chat"."conversation" ("customer_id");
CREATE INDEX IF NOT EXISTS "conversation_vendor_id_idx" ON "chat"."conversation" ("vendor_id");
-- DESC for inbox sorted by most recent activity
CREATE INDEX IF NOT EXISTS "conversation_last_message_at_idx" ON "chat"."conversation" ("last_message_at" DESC);

ALTER TABLE "chat"."conversation"
    ADD CONSTRAINT "conversation_customer_id_fkey"
    FOREIGN KEY ("customer_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
ALTER TABLE "chat"."conversation"
    ADD CONSTRAINT "conversation_vendor_id_fkey"
    FOREIGN KEY ("vendor_id") REFERENCES "account"."vendor" ("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- Individual message within a conversation. sender_id is the account that sent it.
-- metadata stores type-specific data (e.g. image URL and dimensions for Image messages,
-- action payload for System messages).
CREATE TABLE IF NOT EXISTS "chat"."message" (
    "id"              BIGSERIAL NOT NULL,
    "conversation_id" UUID NOT NULL,
    -- The account (buyer or seller) who sent this message
    "sender_id"       UUID NOT NULL,
    "type"            "chat"."message_type" NOT NULL DEFAULT 'Text',
    "content"         TEXT NOT NULL,
    "status"          "chat"."message_status" NOT NULL DEFAULT 'Sent',
    -- Type-specific payload (e.g. {"url": "...", "width": 800} for Image)
    "metadata"        JSONB,
    "date_created"    TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "message_pkey" PRIMARY KEY ("id")
);

-- Composite index supports paginated message history queries within a conversation
CREATE INDEX IF NOT EXISTS "message_conversation_id_idx" ON "chat"."message" ("conversation_id", "date_created" DESC);
CREATE INDEX IF NOT EXISTS "message_sender_id_idx" ON "chat"."message" ("sender_id");

ALTER TABLE "chat"."message"
    ADD CONSTRAINT "message_conversation_id_fkey"
    FOREIGN KEY ("conversation_id") REFERENCES "chat"."conversation" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
ALTER TABLE "chat"."message"
    ADD CONSTRAINT "message_sender_id_fkey"
    FOREIGN KEY ("sender_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
