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
