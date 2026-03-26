-- Recreate type enum
CREATE TYPE "account"."type" AS ENUM ('Customer', 'Vendor');

-- Re-add type column
ALTER TABLE "account"."account" ADD COLUMN "type" "account"."type" NOT NULL DEFAULT 'Customer';

-- Recreate customer table
CREATE TABLE IF NOT EXISTS "account"."customer" (
    "id" UUID NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "customer_pkey" PRIMARY KEY ("id")
);

-- Recreate vendor table
CREATE TABLE IF NOT EXISTS "account"."vendor" (
    "id" UUID NOT NULL,
    "description" TEXT NOT NULL DEFAULT '',
    CONSTRAINT "vendor_pkey" PRIMARY KEY ("id")
);

-- Drop description from profile
ALTER TABLE "account"."profile" DROP COLUMN IF EXISTS "description";
