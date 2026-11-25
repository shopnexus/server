-- The client's Idempotency-Key of a sign-in and of a code mail, so a retry
-- finds the session or the challenge it made; NULL for rows from before.
ALTER TABLE sessions
    ADD COLUMN request_key uuid,
    ADD CONSTRAINT sessions_account_request_key UNIQUE (account_id, request_key);
-- The Idempotency-Key of the sign-up that made the account, so its retry
-- answers is_new as the first request did.
ALTER TABLE credentials ADD COLUMN request_key uuid;
ALTER TABLE otp_challenges
    ADD COLUMN request_key uuid,
    ADD CONSTRAINT otp_challenges_destination_request_key UNIQUE (destination, request_key);
