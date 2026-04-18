ALTER TABLE "catalog"."product_sku" DROP COLUMN IF EXISTS "currency";
UPDATE "catalog"."product_sku" SET "price" = "price" * 1000000000 WHERE "price" != 0;
