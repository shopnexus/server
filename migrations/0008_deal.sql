-- Deals: a buyer and a seller agreeing a price on one listing. A deal holds
-- at most one open price, the buyer's offer or the seller's counter;
-- deal_moves keeps every move for the thread.

CREATE TABLE deals (
    id           uuid PRIMARY KEY,
    listing_id   uuid NOT NULL REFERENCES listings (id),
    buyer_id     uuid NOT NULL REFERENCES accounts (id),
    seller_id    uuid NOT NULL REFERENCES accounts (id),
    status       text NOT NULL CHECK (status IN ('negotiating', 'agreed')),
    open_by      text CHECK (open_by IN ('buyer', 'seller')),
    open_amount  bigint,
    open_at      timestamptz,
    -- The listed price when the open price was made.
    listed_price bigint NOT NULL,
    agreed_price bigint,
    agreed_at    timestamptz,
    write_id     uuid NOT NULL,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,
    UNIQUE (listing_id, buyer_id),
    CHECK ((open_by IS NULL) = (open_amount IS NULL) AND (open_by IS NULL) = (open_at IS NULL)),
    CHECK ((status = 'agreed') = (agreed_price IS NOT NULL) AND (agreed_price IS NULL) = (agreed_at IS NULL))
);

CREATE INDEX deals_buyer ON deals (buyer_id, id);
CREATE INDEX deals_seller ON deals (seller_id, id);

CREATE TABLE deal_moves (
    id       uuid PRIMARY KEY,
    deal_id  uuid NOT NULL REFERENCES deals (id),
    kind     text NOT NULL CHECK (kind IN (
        'offer', 'counter', 'accept', 'decline', 'withdraw', 'cancel', 'price_changed', 'offers_closed',
        'listing_closed'
    )),
    by_party text NOT NULL CHECK (by_party IN ('buyer', 'seller', 'system')),
    -- The offered or agreed price, or the new listed price; NULL otherwise.
    amount   bigint,
    moved_at timestamptz NOT NULL
);

CREATE INDEX deal_moves_deal ON deal_moves (deal_id, id);
