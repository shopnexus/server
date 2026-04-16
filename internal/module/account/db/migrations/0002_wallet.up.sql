CREATE TYPE account.wallet_transaction_type AS ENUM ('Refund', 'Payment', 'TopUp');

CREATE TABLE account.wallet (
    account_id  UUID PRIMARY KEY,
    balance     BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT wallet_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE account.wallet_transaction (
    id            BIGSERIAL PRIMARY KEY,
    account_id    UUID NOT NULL REFERENCES account.wallet(account_id),
    type          account.wallet_transaction_type NOT NULL,
    amount        BIGINT NOT NULL,
    reference_id  TEXT,
    note          TEXT,
    date_created  TIMESTAMPTZ(3) NOT NULL DEFAULT now()
);

CREATE INDEX idx_wallet_tx_account ON account.wallet_transaction(account_id, date_created DESC);
