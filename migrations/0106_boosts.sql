-- Boosts (UC-LST-03): a seller pays once through the payment gateway for a
-- package that moves a listing to the top of the newest order every day.

-- Module payment: one payment per purchase, refunded in full or not at all.
CREATE TABLE payments (
    id          uuid PRIMARY KEY,
    account_id  uuid NOT NULL REFERENCES accounts (id),
    product     text NOT NULL CHECK (product IN ('boost')),
    product_id  uuid NOT NULL,
    amount      bigint NOT NULL CHECK (amount > 0),
    gateway     text NOT NULL CHECK (gateway IN ('vnpay', 'fake')),
    status      text NOT NULL CHECK (status IN ('pending', 'paid', 'failed', 'expired', 'refunding', 'refunded')),
    pay_url     text NOT NULL CHECK (pay_url <> ''),
    gateway_ref text NOT NULL DEFAULT '',
    bank_code   text NOT NULL DEFAULT '',
    result_code text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL,
    paid_at     timestamptz,
    closed_at   timestamptz,
    refunded_at timestamptz,
    write_id    uuid NOT NULL,
    UNIQUE (product, product_id),
    CHECK (expires_at > created_at),
    CHECK ((status IN ('paid', 'refunding', 'refunded')) = (paid_at IS NOT NULL)),
    CHECK ((status = 'refunded') = (refunded_at IS NOT NULL))
);
CREATE INDEX payments_account ON payments (account_id);

-- Module boost: one purchase of a package for one listing.
CREATE TABLE boosts (
    id           uuid PRIMARY KEY,
    listing_id   uuid NOT NULL REFERENCES listings (id),
    seller_id    uuid NOT NULL REFERENCES accounts (id),
    request_key  uuid NOT NULL,
    package_code text NOT NULL CHECK (package_code <> ''),
    run_days     int NOT NULL CHECK (run_days > 0),
    price        bigint NOT NULL CHECK (price > 0),
    status       text NOT NULL CHECK (status IN (
        'awaiting_payment', 'active', 'ended', 'failed', 'expired', 'cancelled',
        'refunding', 'refunded'
    )),
    payment_id   uuid NOT NULL REFERENCES payments (id),
    bumps        int NOT NULL DEFAULT 0 CHECK (bumps >= 0),
    created_at   timestamptz NOT NULL,
    started_at   timestamptz,
    ends_at      timestamptz,
    next_tick_at timestamptz,
    last_bump_at timestamptz,
    ended_at     timestamptz,
    refunded_at  timestamptz,
    write_id     uuid NOT NULL,
    UNIQUE (seller_id, request_key),
    UNIQUE (payment_id),
    CHECK ((status = 'active') = (next_tick_at IS NOT NULL)),
    CHECK ((started_at IS NULL) = (ends_at IS NULL)),
    CHECK ((status = 'refunded') = (refunded_at IS NOT NULL))
);
-- One package awaiting payment or running per listing.
CREATE UNIQUE INDEX boosts_live ON boosts (listing_id) WHERE status IN ('awaiting_payment', 'active');
CREATE INDEX boosts_listing ON boosts (listing_id, id DESC);
CREATE INDEX boosts_seller ON boosts (seller_id);

-- A bump moves a listing up the newest order without changing when it was
-- posted. fresh_at, the later of the two, is what the newest order sorts by.
ALTER TABLE listings ADD COLUMN bumped_at timestamptz;
ALTER TABLE listings ADD COLUMN fresh_at timestamptz GENERATED ALWAYS AS (greatest(published_at, bumped_at)) STORED;

-- The index of 0081 with fresh_at, which the newest order reads.
DROP INDEX listings_search;
CREATE INDEX listings_search ON listings USING paradedb (
    id, title, description, specs_text,
    (title::pdb.unicode_words('alias=title_folded', 'ascii_folding=true')),
    (description::pdb.unicode_words('alias=description_folded', 'ascii_folding=true')),
    (specs_text::pdb.unicode_words('alias=specs_folded', 'ascii_folding=true')),
    (category_code::pdb.literal), (category_root::pdb.literal), price, (condition::pdb.literal),
    allow_offers, (status::pdb.literal), published_at, fresh_at, lat, lng
);
-- The seller's public list, newest first.
CREATE INDEX listings_seller_fresh ON listings (seller_id, fresh_at DESC, id DESC);
