-- Drops all system schema objects in reverse dependency order.

-- Indexes
DROP INDEX IF EXISTS "outbox_event_date_created_idx";

-- Tables
DROP TABLE IF EXISTS "system"."outbox_event";

DROP SCHEMA IF EXISTS "system";
