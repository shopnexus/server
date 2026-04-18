UPDATE "order"."payment" SET "amount" = "amount" / 1000000000 WHERE "amount" != 0;
UPDATE "order"."transport" SET "cost" = "cost" / 1000000000 WHERE "cost" != 0;
UPDATE "order"."order" SET
    "product_cost" = "product_cost" / 1000000000,
    "product_discount" = "product_discount" / 1000000000,
    "transport_cost" = "transport_cost" / 1000000000,
    "total" = "total" / 1000000000
WHERE "product_cost" != 0 OR "product_discount" != 0 OR "transport_cost" != 0 OR "total" != 0;
UPDATE "order"."item" SET
    "unit_price" = "unit_price" / 1000000000,
    "paid_amount" = "paid_amount" / 1000000000,
    "transport_cost_estimate" = "transport_cost_estimate" / 1000000000
WHERE "unit_price" != 0 OR "paid_amount" != 0 OR "transport_cost_estimate" != 0;
UPDATE "order"."refund" SET "amount" = "amount" / 1000000000 WHERE "amount" IS NOT NULL AND "amount" != 0;

ALTER TABLE "order"."payment" ADD COLUMN "buyer_currency" VARCHAR(3) NOT NULL DEFAULT 'VND';
ALTER TABLE "order"."payment" ADD COLUMN "seller_currency" VARCHAR(3) NOT NULL DEFAULT 'VND';
ALTER TABLE "order"."payment" ADD COLUMN "exchange_rate" NUMERIC NOT NULL DEFAULT 1;
