ALTER TABLE "account"."notification"
    DROP COLUMN IF EXISTS "title",
    DROP COLUMN IF EXISTS "metadata";
