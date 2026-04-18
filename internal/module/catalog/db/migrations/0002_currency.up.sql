UPDATE "catalog"."product_sku" SET "price" = "price" / 1000000000 WHERE "price" != 0;
ALTER TABLE "catalog"."product_sku" ADD COLUMN "currency" VARCHAR(3) NOT NULL DEFAULT 'VND';
