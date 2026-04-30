-- name: GetInternalBalance :one
SELECT "balance" FROM "order"."internal_wallet" WHERE "id" = @id;

-- name: DebitInternalWallet :one
-- Deducts min(balance, amount). Returns new balance after the deduction.
UPDATE "order"."internal_wallet"
SET "balance" = GREATEST("balance" - @amount::BIGINT, 0)
WHERE "id" = @id
RETURNING "balance" AS new_balance;

-- name: CreditInternalWallet :one
UPDATE "order"."internal_wallet"
SET "balance" = "balance" + @amount::BIGINT
WHERE "id" = @id
RETURNING "balance" AS new_balance;