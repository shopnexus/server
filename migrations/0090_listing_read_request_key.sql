-- The account's Idempotency-Key of a read's start, so a retry finds the
-- read it started; NULL for reads started before.
ALTER TABLE listing_reads
    ADD COLUMN request_key uuid,
    ADD CONSTRAINT listing_reads_account_request_key UNIQUE (account_id, request_key);
