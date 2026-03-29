-- Custom payment method queries

-- name: UnsetDefaultPaymentMethod :exec
UPDATE "account"."payment_method"
SET "is_default" = false
WHERE "account_id" = @account_id AND "is_default" = true;

-- name: SetDefaultPaymentMethod :one
UPDATE "account"."payment_method"
SET "is_default" = true
WHERE "id" = @id AND "account_id" = @account_id
RETURNING *;
