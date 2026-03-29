-- =============================================
-- Module: System
-- Schema: system
-- Description: System-level infrastructure tables. Currently holds the
--              transactional outbox for reliable event publishing to NATS
--              (or any message broker) without two-phase commit.
-- =============================================

CREATE SCHEMA IF NOT EXISTS "system";

-- Tables

-- Transactional outbox for reliable at-least-once event delivery.
-- Events are written in the same DB transaction as the business operation,
-- then a background poller reads unprocessed rows and publishes them to NATS.
-- date_processed is set once the event is successfully published.
CREATE TABLE IF NOT EXISTS "system"."outbox_event" (
    "id" BIGSERIAL NOT NULL,
    -- NATS subject / message broker topic
    "topic" VARCHAR(100) NOT NULL,
    -- Event payload published to the topic
    "data" JSONB NOT NULL,
    "processed" BOOLEAN NOT NULL DEFAULT false,
    -- NULL until the event has been successfully published
    "date_processed" TIMESTAMPTZ(3),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "outbox_event_pkey" PRIMARY KEY ("id")
);

-- Indexes

-- For the outbox poller to page through unprocessed events in insertion order
CREATE INDEX IF NOT EXISTS "outbox_event_date_created_idx" ON "system"."outbox_event" ("date_created");
