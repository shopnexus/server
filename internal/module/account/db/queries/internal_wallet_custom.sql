-- name: GetInternalBalance :one
SELECT "internal_balance" FROM "account"."profile" WHERE "id" = @account_id;

-- name: DebitInternalBalance :one
-- Atomic debit. Locks the row, deducts min(balance, amount), returns
-- (old, new). Single-statement so concurrent debits serialize correctly —
-- the previous "SELECT then UPDATE inside BeginTx" pattern misreported
-- "deducted" under contention because it had no FOR UPDATE.
WITH locked AS (
    SELECT p."id" AS account_id, p."internal_balance" AS old_balance
    FROM "account"."profile" p
    WHERE p."id" = @account_id
    FOR UPDATE
),
updated AS (
    UPDATE "account"."profile" p
    SET "internal_balance" = GREATEST(p."internal_balance" - @amount::BIGINT, 0)
    FROM locked
    WHERE p."id" = locked.account_id
    RETURNING p."internal_balance" AS new_balance
)
SELECT locked.old_balance, updated.new_balance
FROM locked, updated;

-- name: CreditInternalBalance :one
UPDATE "account"."profile"
SET "internal_balance" = "internal_balance" + @amount::BIGINT
WHERE "id" = @account_id
RETURNING "internal_balance" AS new_balance;
