-- Merge vendor description into profile
ALTER TABLE "account"."profile"
    ADD COLUMN IF NOT EXISTS "description" TEXT NOT NULL DEFAULT '';

-- Copy existing vendor descriptions to profile
UPDATE "account"."profile" p
SET "description" = v."description"
FROM "account"."vendor" v
WHERE p.id = v.id;

-- Drop account type column (no longer needed)
ALTER TABLE "account"."account" DROP COLUMN IF EXISTS "type";

-- Drop type enum
DROP TYPE IF EXISTS "account"."type";

-- Drop customer and vendor tables
DROP TABLE IF EXISTS "account"."customer" CASCADE;
DROP TABLE IF EXISTS "account"."vendor" CASCADE;
