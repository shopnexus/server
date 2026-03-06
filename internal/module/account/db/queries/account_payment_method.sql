-- name: GetPaymentMethod :one
SELECT *
FROM "account"."payment_method"
WHERE "id" = sqlc.arg('id') AND "account_id" = sqlc.arg('account_id');

-- name: ListCountPaymentMethod :many
SELECT sqlc.embed(embed_payment_method), COUNT(*) OVER() as total_count
FROM "account"."payment_method" embed_payment_method
WHERE (
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL)
)
ORDER BY "is_default" DESC, "date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateDefaultPaymentMethod :one
INSERT INTO "account"."payment_method" ("account_id", "type", "label", "data", "is_default")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdatePaymentMethod :one
UPDATE "account"."payment_method"
SET "type" = COALESCE(sqlc.narg('type'), "type"),
    "label" = COALESCE(sqlc.narg('label'), "label"),
    "data" = COALESCE(sqlc.narg('data'), "data"),
    "is_default" = COALESCE(sqlc.narg('is_default'), "is_default"),
    "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = sqlc.arg('id') AND "account_id" = sqlc.arg('account_id')
RETURNING *;

-- name: DeletePaymentMethod :exec
DELETE FROM "account"."payment_method"
WHERE "id" = sqlc.arg('id') AND "account_id" = sqlc.arg('account_id');

-- name: UnsetDefaultPaymentMethod :exec
UPDATE "account"."payment_method"
SET "is_default" = false, "date_updated" = CURRENT_TIMESTAMP
WHERE "account_id" = sqlc.arg('account_id') AND "is_default" = true;

-- name: SetDefaultPaymentMethod :one
UPDATE "account"."payment_method"
SET "is_default" = true, "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = sqlc.arg('id') AND "account_id" = sqlc.arg('account_id')
RETURNING *;
