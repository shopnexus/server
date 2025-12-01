CREATE SCHEMA IF NOT EXISTS "system";

CREATE TABLE IF NOT EXISTS "system"."outbox_event" (
    "id" BIGSERIAL NOT NULL,
    "topic" VARCHAR(100) NOT NULL,
    "data" JSONB NOT NULL,
    "processed" BOOLEAN NOT NULL DEFAULT false,
    "date_processed" TIMESTAMPTZ(3),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "outbox_event_pkey" PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "outbox_event_date_created_idx" ON "system"."outbox_event" ("date_created");

