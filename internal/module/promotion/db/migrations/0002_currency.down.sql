ALTER TABLE "promotion"."promotion" DROP COLUMN IF EXISTS "currency";

UPDATE "promotion"."promotion" SET "data" = jsonb_set("data", '{min_spend}',
    to_jsonb(("data"->>'min_spend')::bigint * 1000000000))
WHERE "data" IS NOT NULL AND "data"->>'min_spend' IS NOT NULL AND ("data"->>'min_spend')::bigint != 0;

UPDATE "promotion"."promotion" SET "data" = jsonb_set("data", '{max_discount}',
    to_jsonb(("data"->>'max_discount')::bigint * 1000000000))
WHERE "data" IS NOT NULL AND "data"->>'max_discount' IS NOT NULL AND ("data"->>'max_discount')::bigint != 0;

UPDATE "promotion"."promotion" SET "data" = jsonb_set("data", '{discount_price}',
    to_jsonb(("data"->>'discount_price')::bigint * 1000000000))
WHERE "data" IS NOT NULL AND "data"->>'discount_price' IS NOT NULL AND ("data"->>'discount_price')::bigint != 0;
