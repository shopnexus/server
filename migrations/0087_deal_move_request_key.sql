-- The Idempotency-Key of the request that made a party's move, so a retry
-- finds the move it made; NULL for the system's moves and older ones.
ALTER TABLE deal_moves
    ADD COLUMN request_key uuid,
    ADD CONSTRAINT deal_moves_request_key UNIQUE (deal_id, by_party, request_key);
