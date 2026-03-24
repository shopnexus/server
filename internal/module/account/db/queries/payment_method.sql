-- name: UnsetDefaultPaymentMethod :exec
UPDATE "account"."payment_method"
SET "is_default" = false
WHERE "account_id" = sqlc.arg('account_id') AND "is_default" = true;

-- name: SetDefaultPaymentMethod :one
UPDATE "account"."payment_method"
SET "is_default" = true
WHERE "id" = sqlc.arg('id') AND "account_id" = sqlc.arg('account_id')
RETURNING *;
