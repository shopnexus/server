-- name: UpdateProfileCountry :execrows
UPDATE "account"."profile"
SET "country" = @country
WHERE "id" = @id AND "balance" = 0;

-- name: CreateSignupProfile :one
INSERT INTO "account"."profile" ("id", "country", "name")
VALUES (@id, @country, @name)
RETURNING *;

-- name: GetProfileBalance :one
SELECT "balance" FROM "account"."profile" WHERE "id" = @id;

-- name: DebitProfileBalance :one
-- Deducts min(balance, amount). Returns new balance after the deduction.
UPDATE "account"."profile"
SET "balance" = GREATEST("balance" - @amount::BIGINT, 0)
WHERE "id" = @id
RETURNING "balance" AS new_balance;

-- name: CreditProfileBalance :one
UPDATE "account"."profile"
SET "balance" = "balance" + @amount::BIGINT
WHERE "id" = @id
RETURNING "balance" AS new_balance;

-- name: SetAccountDefaultContact :exec
UPDATE "account"."profile"
SET "default_contact_id" = @default_contact_id
WHERE "id" = @id;

-- name: GetAccountDefaults :one
SELECT "default_contact_id", "default_wallet_id" FROM "account"."profile" WHERE "id" = @id;
