-- A listing's price is at most 1,000,000,000 đồng (decided 2026-10-26).

ALTER TABLE listings DROP CONSTRAINT listings_price_check;
ALTER TABLE listings ADD CONSTRAINT listings_price_check CHECK (price BETWEEN 10000 AND 1000000000);
