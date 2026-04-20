-- name: GetExchangeRatesByBase :many
SELECT "base", "target", "rate", "fetched_at"
FROM "common"."exchange_rate"
WHERE "base" = $1
ORDER BY "target";

-- name: UpsertExchangeRate :exec
INSERT INTO "common"."exchange_rate" ("base", "target", "rate", "fetched_at")
VALUES ($1, $2, $3, $4)
ON CONFLICT ("base", "target") DO UPDATE
SET "rate" = EXCLUDED."rate",
    "fetched_at" = EXCLUDED."fetched_at",
    "date_updated" = CURRENT_TIMESTAMP;
