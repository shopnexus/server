-- Drops all chat schema objects in reverse dependency order.
-- Messages reference conversations, so messages are dropped first.

-- Tables (child tables first)
DROP TABLE IF EXISTS "chat"."message";
DROP TABLE IF EXISTS "chat"."conversation";

-- Enums
DROP TYPE IF EXISTS "chat"."message_status";
DROP TYPE IF EXISTS "chat"."message_type";

DROP SCHEMA IF EXISTS "chat";
