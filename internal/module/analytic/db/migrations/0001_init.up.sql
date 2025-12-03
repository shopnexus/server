CREATE SCHEMA IF NOT EXISTS "analytic";

CREATE TYPE "analytic"."interaction_ref_type" AS ENUM ('Product', 'Category', 'Brand');

CREATE TABLE IF NOT EXISTS "analytic"."interaction" (
    "id" BIGSERIAL NOT NULL,
    "account_id" UUID,
    "session_id" TEXT,
    "event_type" TEXT NOT NULL,
    "ref_type" "analytic"."interaction_ref_type" NOT NULL,
    "ref_id" TEXT NOT NULL,
    "metadata" JSONB,
    "user_agent" TEXT,
    "ip_address" TEXT,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "interaction_pkey" PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "interaction_account_id_idx" ON "analytic"."interaction" ("account_id");
CREATE INDEX IF NOT EXISTS "interaction_session_id_idx" ON "analytic"."interaction" ("session_id");
CREATE INDEX IF NOT EXISTS "interaction_event_type_idx" ON "analytic"."interaction" ("event_type");
CREATE INDEX IF NOT EXISTS "interaction_ref_type_ref_id_idx" ON "analytic"."interaction" ("ref_type", "ref_id");