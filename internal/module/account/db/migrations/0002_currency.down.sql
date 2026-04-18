ALTER TABLE "account"."account" DROP COLUMN IF EXISTS "settings";
UPDATE "account"."wallet" SET "balance" = "balance" * 1000000000 WHERE "balance" != 0;
UPDATE "account"."wallet_transaction" SET "amount" = "amount" * 1000000000 WHERE "amount" != 0;
