-- The seller's Idempotency-Key of a post, so a retry finds the listing it
-- made; NULL for listings posted before and those a seed wrote.
ALTER TABLE listings
    ADD COLUMN request_key uuid,
    ADD CONSTRAINT listings_seller_request_key UNIQUE (seller_id, request_key);
