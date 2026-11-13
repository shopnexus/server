-- Module catalog: a seller's listing drafts, kept so their other phones
-- find them (decided 2026-11-13). The server reads the typed columns; body
-- is the app's JSON as a string, laid out as schema_version says, and the
-- server never reads it. photos are upload IDs of module media, in order;
-- like other modules' tables, this one references no table of media. The
-- last write wins. A draft past expires_at, 30 days after its last write,
-- is deleted by the account's next list or write.
CREATE TABLE listing_drafts (
    id             uuid PRIMARY KEY,
    account_id     uuid NOT NULL REFERENCES accounts (id),
    schema_version integer NOT NULL CHECK (schema_version >= 1),
    title          text NOT NULL CHECK (char_length(title) <= 100),
    price          bigint CHECK (price >= 0),
    photos         uuid[] NOT NULL CHECK (cardinality(photos) <= 10),
    body           text NOT NULL CHECK (char_length(body) <= 32768),
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL,
    expires_at     timestamptz NOT NULL
);
CREATE INDEX listing_drafts_account_updated ON listing_drafts (account_id, updated_at DESC);
