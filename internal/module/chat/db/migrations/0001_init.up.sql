-- =============================================
-- Module: Chat
-- Schema: chat
-- Description: Real-time messaging between buyer and seller accounts.
--              Each conversation is a 1-1 thread; messages support text,
--              images, and system-generated events.
-- Note: conversation still uses legacy buyer_id / seller_id column names
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
CREATE TABLE IF NOT EXISTS "chat"."conversation" (
    "id"              UUID NOT NULL DEFAULT gen_random_uuid(),
    -- The account acting as buyer in this conversation
    "buyer_id"        UUID NOT NULL,
    -- The account acting as seller in this conversation
    "seller_id"       UUID NOT NULL,
    -- Denormalized timestamp of the most recent message for inbox ordering
    "last_message_at" TIMESTAMPTZ(3),
    "date_created"    TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "conversation_pkey" PRIMARY KEY ("id"),
    -- Only one conversation thread per (buyer, seller) pair
    CONSTRAINT "conversation_buyer_seller_key" UNIQUE ("buyer_id", "seller_id")
);
-- buyer_id is covered by conversation_buyer_seller_key's leading column
CREATE INDEX IF NOT EXISTS "conversation_seller_id_idx" ON "chat"."conversation" ("seller_id");
-- DESC for inbox sorted by most recent activity
CREATE INDEX IF NOT EXISTS "conversation_last_message_at_idx" ON "chat"."conversation" ("last_message_at" DESC);

-- Individual message within a conversation.
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
    CONSTRAINT "message_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "message_conversation_id_fkey" FOREIGN KEY ("conversation_id")
        REFERENCES "chat"."conversation" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
-- Composite index supports paginated message history queries within a conversation
CREATE INDEX IF NOT EXISTS "message_conversation_id_idx" ON "chat"."message" ("conversation_id", "date_created" DESC);
CREATE INDEX IF NOT EXISTS "message_sender_id_idx" ON "chat"."message" ("sender_id");
