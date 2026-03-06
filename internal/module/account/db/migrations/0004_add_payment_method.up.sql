CREATE TABLE IF NOT EXISTS "account"."payment_method" (
    "id"           UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id"   UUID NOT NULL,
    "type"         VARCHAR(50) NOT NULL,
    "label"        VARCHAR(100) NOT NULL,
    "data"         JSONB NOT NULL,
    "is_default"   BOOLEAN NOT NULL DEFAULT false,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "payment_method_pkey" PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "payment_method_account_id_idx" ON "account"."payment_method" ("account_id");
CREATE UNIQUE INDEX IF NOT EXISTS "payment_method_account_default_key"
    ON "account"."payment_method" ("account_id") WHERE "is_default" = true;

ALTER TABLE "account"."payment_method"
    ADD CONSTRAINT "payment_method_account_id_fkey"
    FOREIGN KEY ("account_id") REFERENCES "account"."account" ("id") ON DELETE CASCADE ON UPDATE CASCADE;
