-- Account erasure: a deleted account keeps its row, without a name, bio or
-- avatar, for the orders, reviews and messages that point to it.
ALTER TABLE accounts DROP CONSTRAINT accounts_status_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_status_check
CHECK (status IN ('active', 'suspended', 'deleted'));
ALTER TABLE accounts ADD COLUMN deleted_at timestamptz;
ALTER TABLE accounts ADD CONSTRAINT accounts_deleted CHECK (
    (status = 'deleted') = (deleted_at IS NOT NULL)
    AND (status <> 'deleted' OR (display_name IS NULL AND bio IS NULL AND avatar_key IS NULL))
);

-- Feedback a person sends from Trợ giúp. request_key is the client's
-- Idempotency-Key, so a repeated tap adds nothing. At most 10 per account
-- in 24 hours.
CREATE TABLE feedback (
    id          uuid PRIMARY KEY,
    account_id  uuid NOT NULL REFERENCES accounts (id),
    body        text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 1000),
    request_key uuid NOT NULL,
    created_at  timestamptz NOT NULL,
    UNIQUE (account_id, request_key)
);
CREATE INDEX feedback_account_created ON feedback (account_id, created_at);

-- A person's last 10 text searches. query_key folds case, spaces and
-- Vietnamese marks, so the same words are one entry; a new search of them
-- takes a new id and moves the entry to the top.
CREATE TABLE search_history (
    id          uuid PRIMARY KEY,
    account_id  uuid NOT NULL REFERENCES accounts (id),
    query_key   text NOT NULL,
    query       text NOT NULL CHECK (char_length(query) BETWEEN 1 AND 200),
    category    text,
    photo_url   text,
    searched_at timestamptz NOT NULL,
    UNIQUE (account_id, query_key)
);
CREATE INDEX search_history_account ON search_history (account_id, id);
