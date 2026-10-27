-- Address book: where a person's parcels go, at most 10 per account, one of
-- them the default. A COD request copies an address into its order, so an
-- address leaves the table when its owner deletes it. request_key is the
-- Idempotency-Key of the request that added the address, so a retried
-- request finds it instead of adding a second one.

CREATE TABLE addresses (
    id             uuid PRIMARY KEY,
    account_id     uuid NOT NULL REFERENCES accounts (id),
    recipient_name text NOT NULL,
    phone          text NOT NULL,
    street         text NOT NULL,
    commune_code   text NOT NULL REFERENCES communes (code),
    is_default     boolean NOT NULL,
    request_key    uuid NOT NULL,
    write_id       uuid NOT NULL,
    created_at     timestamptz NOT NULL,
    UNIQUE (account_id, request_key)
);

-- At most one default per account; the repository keeps one whenever the
-- account has an address.
CREATE UNIQUE INDEX addresses_default ON addresses (account_id) WHERE is_default;
CREATE INDEX addresses_account ON addresses (account_id, id);
