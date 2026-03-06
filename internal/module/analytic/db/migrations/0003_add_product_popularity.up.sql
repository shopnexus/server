CREATE TABLE IF NOT EXISTS "analytic"."product_popularity" (
    "spu_id"         UUID NOT NULL,
    "score"          DOUBLE PRECISION NOT NULL DEFAULT 0,
    "view_count"     BIGINT NOT NULL DEFAULT 0,
    "purchase_count" BIGINT NOT NULL DEFAULT 0,
    "favorite_count" BIGINT NOT NULL DEFAULT 0,
    "cart_count"     BIGINT NOT NULL DEFAULT 0,
    "review_count"   BIGINT NOT NULL DEFAULT 0,
    "date_updated"   TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "product_popularity_pkey" PRIMARY KEY ("spu_id")
);

CREATE INDEX IF NOT EXISTS "product_popularity_score_idx" ON "analytic"."product_popularity" ("score" DESC);
