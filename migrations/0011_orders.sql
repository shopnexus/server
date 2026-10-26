-- COD orders: a buyer's request for cash on delivery on an agreed deal,
-- shipped by the seller on a carrier's own app. A deal has at most one open
-- order and keeps its earlier ones; a listing has at most one shipped
-- order, the one it is reserved for.

CREATE SEQUENCE order_codes START 100001;

CREATE TABLE orders (
    id                uuid PRIMARY KEY,
    code              text NOT NULL UNIQUE DEFAULT 'SN-' || nextval('order_codes'),
    deal_id           uuid NOT NULL REFERENCES deals (id),
    listing_id        uuid NOT NULL REFERENCES listings (id),
    buyer_id          uuid NOT NULL REFERENCES accounts (id),
    seller_id         uuid NOT NULL REFERENCES accounts (id),
    status            text NOT NULL
    CHECK (status IN ('requested', 'shipped', 'delivered', 'returned', 'cancelled')),
    price             bigint NOT NULL,
    recipient_name    text NOT NULL,
    recipient_phone   text NOT NULL,
    recipient_street  text NOT NULL,
    recipient_commune text NOT NULL REFERENCES communes (code),
    -- The waybill, set once the order is shipped. source is self until a
    -- carrier is integrated.
    waybill_source    text CHECK (waybill_source IN ('self')),
    carrier           text CHECK (carrier IN ('ghtk', 'ghn', 'spx', 'jt', 'viettel_post', 'other')),
    carrier_name      text,
    tracking_code     text,
    fee               bigint,
    cancelled_by      text CHECK (cancelled_by IN ('buyer', 'seller', 'system')),
    cancel_reason     text CHECK (cancel_reason IN ('buyer', 'seller', 'sent_to_another', 'listing_closed')),
    requested_at      timestamptz NOT NULL,
    shipped_at        timestamptz,
    closed_at         timestamptz
);

CREATE UNIQUE INDEX orders_open_per_deal ON orders (deal_id) WHERE status IN ('requested', 'shipped');
CREATE UNIQUE INDEX orders_shipped_per_listing ON orders (listing_id) WHERE status = 'shipped';
-- A deal's latest order, read with every deal.
CREATE INDEX orders_deal ON orders (deal_id, id);
CREATE INDEX orders_buyer ON orders (buyer_id, id);
CREATE INDEX orders_seller ON orders (seller_id, id);

-- A sold deal keeps its agreed price.
ALTER TABLE deals DROP CONSTRAINT deals_status_check;
ALTER TABLE deals ADD CONSTRAINT deals_status_check CHECK (status IN ('negotiating', 'agreed', 'sold'));
ALTER TABLE deals DROP CONSTRAINT deals_check1;
ALTER TABLE deals ADD CONSTRAINT deals_agreed_check
CHECK ((status IN ('agreed', 'sold')) = (agreed_price IS NOT NULL) AND (agreed_price IS NULL) = (agreed_at IS NULL));

ALTER TABLE deal_moves DROP CONSTRAINT deal_moves_kind_check;
ALTER TABLE deal_moves ADD CONSTRAINT deal_moves_kind_check CHECK (kind IN (
    'offer', 'counter', 'accept', 'decline', 'withdraw', 'cancel', 'price_changed', 'offers_closed',
    'listing_closed', 'cod_requested', 'shipped', 'delivered', 'returned', 'order_cancelled'
));
