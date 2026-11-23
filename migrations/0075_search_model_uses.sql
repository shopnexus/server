-- Discovery: how many times the search model read a signed-in person's
-- queries on one UTC day. One row per account; a new day starts it again.
CREATE TABLE search_model_uses (
    account_id uuid PRIMARY KEY REFERENCES accounts (id),
    day        date NOT NULL,
    calls      integer NOT NULL
);
