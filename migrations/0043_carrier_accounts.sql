-- Carrier accounts a seller linked, at most one per carrier, GHTK or GHN.
-- token is the carrier's token sealed with AES-GCM under CARRIER_TOKEN_KEY;
-- the plain token is never stored. Orders keep the pickup of a linked
-- shipment: the carrier's pickup point, or a copy of the seller's address.
-- Spec: docs/superpowers/specs/2026-11-13-carrier-integration-design.md.

CREATE TABLE carrier_accounts (
    account_id uuid        NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    carrier    text        NOT NULL CHECK (carrier IN ('ghtk', 'ghn')),
    shop_code  text        NOT NULL CHECK (char_length(shop_code) BETWEEN 1 AND 50),
    token      bytea       NOT NULL,
    nonce      bytea       NOT NULL,
    linked_at  timestamptz NOT NULL,
    PRIMARY KEY (account_id, carrier)
);

ALTER TABLE orders
    ADD COLUMN pickup_point_id text,
    ADD COLUMN pickup_name     text,
    ADD COLUMN pickup_phone    text,
    ADD COLUMN pickup_street   text,
    ADD COLUMN pickup_commune  text REFERENCES communes (code),
    ADD CONSTRAINT orders_pickup_check CHECK (
        -- A point or a copy of an address, never both; the copy is whole.
        (pickup_point_id IS NULL OR pickup_name IS NULL)
        AND (pickup_name IS NULL) = (pickup_phone IS NULL)
        AND (pickup_name IS NULL) = (pickup_street IS NULL)
        AND (pickup_name IS NULL) = (pickup_commune IS NULL)
        -- Only a linked shipment has a pickup.
        AND ((pickup_point_id IS NULL AND pickup_name IS NULL) OR shipment_mode = 'linked')
    );

-- Unlinking a carrier asks whether a shipment of the seller's at it is open.
CREATE INDEX orders_open_shipments ON orders (seller_id, carrier) WHERE status = 'shipped';
