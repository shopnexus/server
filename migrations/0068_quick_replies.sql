-- Quick replies (2026-11-20): a person's saved chat answers, in their
-- order, at most 20. An account without a row reads the defaults; an
-- empty array is a person who removed them all.
CREATE TABLE quick_replies (
    account_id uuid PRIMARY KEY REFERENCES accounts (id),
    items      text[] NOT NULL CHECK (cardinality(items) <= 20)
);
