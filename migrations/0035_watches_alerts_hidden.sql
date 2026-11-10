-- Search watches, price alerts on saves and hidden listings.
--
-- A watch keeps a query, the phrases the model read from it, the person's
-- filters and a commune; no point of the person. listing_arrivals logs each
-- listing's first publication after it committed, which the sweeps read by
-- seq. A save may carry its own alert level and the last price it told.

CREATE TABLE search_watches (
    id           uuid PRIMARY KEY,
    account_id   uuid NOT NULL REFERENCES accounts (id),
    query_key    text NOT NULL,
    query        text NOT NULL CHECK (char_length(query) BETWEEN 1 AND 200),
    phrases      jsonb NOT NULL,
    category     text,
    price_min    bigint CHECK (price_min > 0),
    price_max    bigint CHECK (price_max > 0),
    conditions   text[] NOT NULL,
    offers_only  boolean NOT NULL,
    commune_code text REFERENCES communes (code),
    radius_km    integer CHECK (radius_km BETWEEN 1 AND 200),
    alert        boolean NOT NULL,
    new_count    integer NOT NULL CHECK (new_count BETWEEN 0 AND 99),
    last_arrival bigint NOT NULL,
    created_at   timestamptz NOT NULL,
    UNIQUE (account_id, query_key),
    CHECK (radius_km IS NULL OR commune_code IS NOT NULL)
);

CREATE INDEX search_watches_account ON search_watches (account_id, id);

CREATE TABLE listing_arrivals (
    seq        bigserial PRIMARY KEY,
    listing_id uuid NOT NULL UNIQUE REFERENCES listings (id),
    -- The insert's time, after the publication committed; a sweep reads
    -- arrivals older than a minute, so a lower seq still committing is not
    -- skipped.
    arrived_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX listing_arrivals_at ON listing_arrivals (arrived_at);

ALTER TABLE saved_listings ADD COLUMN alert_below bigint CHECK (alert_below BETWEEN 10000 AND 1000000000);
ALTER TABLE saved_listings ADD COLUMN alerted_price bigint;

CREATE INDEX saved_listings_listing ON saved_listings (listing_id);

CREATE TABLE saved_settings (
    account_id       uuid PRIMARY KEY REFERENCES accounts (id),
    price_cut_alerts boolean NOT NULL
);

CREATE TABLE hidden_listings (
    id         uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts (id),
    listing_id uuid NOT NULL REFERENCES listings (id),
    UNIQUE (account_id, listing_id)
);

CREATE INDEX hidden_listings_account ON hidden_listings (account_id, id);
