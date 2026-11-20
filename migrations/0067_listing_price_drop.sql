-- The price a listing's last change after publication counts from, and
-- when it changed; cards show a recent drop. Listings start without one,
-- since no price history was kept before.
ALTER TABLE listings
    ADD COLUMN previous_price bigint,
    ADD COLUMN price_changed_at timestamptz,
    ADD CONSTRAINT listings_price_change CHECK ((previous_price IS NULL) = (price_changed_at IS NULL));
