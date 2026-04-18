UPDATE "account"."wallet" SET "balance" = "balance" / 1000000000 WHERE "balance" != 0;
UPDATE "account"."wallet_transaction" SET "amount" = "amount" / 1000000000 WHERE "amount" != 0;
ALTER TABLE "account"."account" ADD COLUMN "settings" JSONB NOT NULL DEFAULT '{}';
