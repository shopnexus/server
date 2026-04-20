-- =============================================
-- Module: Analytic
-- Schema: analytic
-- Description: User interaction event tracking and aggregated
--              product popularity scores used for ranking and recommendations.
-- =============================================

CREATE SCHEMA IF NOT EXISTS "analytic";

-- Enums

-- The kind of entity a user interaction is associated with
CREATE TYPE "analytic"."interaction_ref_type" AS ENUM ('Product', 'Category');

-- Tables

-- Raw event log of every user interaction (views, clicks, purchases, favorites, etc.).
CREATE TABLE IF NOT EXISTS "analytic"."interaction" (
  "id" BIGSERIAL NOT NULL,
  -- Null for unauthenticated users
  "account_id" UUID,
  -- Sequential account number for denormalized reporting
  "account_number" BIGINT NOT NULL,
  -- Guest session identifier; NULL for authenticated users without a session cookie
  "session_id" TEXT,
  -- Event kind (e.g. 'View', 'AddToCart', 'Purchase', 'Favorite')
  "event_type" TEXT NOT NULL,
  "ref_type" "analytic"."interaction_ref_type" NOT NULL,
  -- ID of the referenced entity (product, category)
  "ref_id" TEXT NOT NULL,
  -- Optional extra event data (e.g. source page, referrer, experiment variant)
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

-- Aggregated popularity counters per product SPU, updated asynchronously.
-- id references catalog.product_spu (no FK enforced across schemas).
CREATE TABLE IF NOT EXISTS "analytic"."product_popularity" (
  "id" UUID NOT NULL,
  -- Weighted composite popularity score for ranking
  "score" DOUBLE PRECISION NOT NULL DEFAULT 0,
  "view_count" BIGINT NOT NULL DEFAULT 0,
  "purchase_count" BIGINT NOT NULL DEFAULT 0,
  "favorite_count" BIGINT NOT NULL DEFAULT 0,
  "cart_count" BIGINT NOT NULL DEFAULT 0,
  "review_count" BIGINT NOT NULL DEFAULT 0,
  "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "product_popularity_pkey" PRIMARY KEY ("id")
);
-- DESC to efficiently query the most popular products
CREATE INDEX IF NOT EXISTS "product_popularity_score_idx" ON "analytic"."product_popularity" ("score" DESC);
