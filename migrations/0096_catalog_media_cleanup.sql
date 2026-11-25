-- The reads before 0039 and their update columns: no release writes them.
DROP TABLE video_reads, link_reads;
ALTER TABLE account_updates DROP COLUMN video_read_id, DROP COLUMN link_read_id;

-- No read prices its draft; the price guide does, and stores nothing.
ALTER TABLE listing_reads DROP COLUMN estimate;

-- Rules modules catalog and media hold in Go, kept by the tables too. NOT
-- VALID: new and updated rows are checked, and an old row never fails the
-- deploy.
ALTER TABLE listings ADD CONSTRAINT listings_reserved_check CHECK (
    (status = 'reserved') = (reserved_for IS NOT NULL)
) NOT VALID;

ALTER TABLE listings ADD CONSTRAINT listings_deleted_check CHECK (
    (status = 'deleted') = (deleted_at IS NOT NULL)
) NOT VALID;

-- A sale keeps sold_at when the listing is deleted; unmarking clears it.
ALTER TABLE listings ADD CONSTRAINT listings_sold_check CHECK (
    (status != 'sold' OR sold_at IS NOT NULL) AND (sold_at IS NULL OR status IN ('sold', 'deleted'))
) NOT VALID;

-- CASE, since jsonb_array_length fails on a scalar.
ALTER TABLE listings ADD CONSTRAINT listings_photos_check CHECK (
    CASE WHEN jsonb_typeof(photos) = 'array' THEN jsonb_array_length(photos) BETWEEN 1 AND 10 ELSE FALSE END
) NOT VALID;

ALTER TABLE listings ADD CONSTRAINT listings_specs_check CHECK (
    CASE WHEN jsonb_typeof(specs) = 'array' THEN jsonb_array_length(specs) <= 20 ELSE FALSE END
) NOT VALID;

-- The price sample's candidates are one category and condition.
CREATE INDEX listings_price_sample ON listings (category_code, condition);

-- released: the holder no longer shows the upload, and its objects go.
-- The new list holds the old, so every row passes and it is checked whole.
ALTER TABLE uploads DROP CONSTRAINT uploads_status_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_status_check CHECK (
    status IN ('waiting', 'attached', 'ready', 'failed', 'expired', 'consumed', 'released')
);

ALTER TABLE uploads ADD CONSTRAINT uploads_ready_check CHECK (
    status != 'ready' OR photo_key IS NOT NULL
) NOT VALID;

ALTER TABLE uploads ADD CONSTRAINT uploads_waiting_check CHECK (
    status != 'waiting' OR holder_id IS NULL
) NOT VALID;
