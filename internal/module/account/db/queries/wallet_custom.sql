-- name: GetWallet :one
SELECT * FROM account.wallet WHERE account_id = $1;

-- name: CreateWallet :one
INSERT INTO account.wallet (account_id, balance)
VALUES ($1, 0)
RETURNING *;

-- name: DebitWallet :one
UPDATE account.wallet
SET balance = CASE
    WHEN balance >= @amount THEN balance - @amount
    ELSE 0
END
WHERE account_id = @account_id
RETURNING *;

-- name: CreditWallet :one
INSERT INTO account.wallet (account_id, balance)
VALUES (@account_id, @amount)
ON CONFLICT (account_id) DO UPDATE
SET balance = account.wallet.balance + @amount
RETURNING *;

-- name: CreateWalletTransaction :one
INSERT INTO account.wallet_transaction (account_id, type, amount, reference_id, note)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListWalletTransactions :many
SELECT * FROM account.wallet_transaction
WHERE account_id = $1
ORDER BY date_created DESC
LIMIT $2 OFFSET $3;
