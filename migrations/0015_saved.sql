-- Saved listings: a person keeps a listing with the price it had, to come
-- back to it. At most 500 per person.

CREATE TABLE saved_listings (
    id          uuid PRIMARY KEY,
    account_id  uuid NOT NULL REFERENCES accounts (id),
    listing_id  uuid NOT NULL REFERENCES listings (id),
    saved_price bigint NOT NULL,
    created_at  timestamptz NOT NULL,
    UNIQUE (account_id, listing_id)
);

CREATE INDEX saved_listings_account ON saved_listings (account_id, id);
