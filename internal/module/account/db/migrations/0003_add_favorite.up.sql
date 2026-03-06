CREATE TABLE IF NOT EXISTS "account"."favorite" (
    "id"           BIGSERIAL NOT NULL,
    "account_id"   UUID NOT NULL,
    "spu_id"       UUID NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "favorite_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "favorite_account_id_spu_id_key" ON "account"."favorite" ("account_id", "spu_id");
CREATE INDEX IF NOT EXISTS "favorite_spu_id_idx" ON "account"."favorite" ("spu_id");

ALTER TABLE "account"."favorite"
    ADD CONSTRAINT "favorite_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
