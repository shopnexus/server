-- The sender's Idempotency-Key of a message, so a retry finds the message
-- it sent; NULL for deal messages and older ones.
ALTER TABLE messages
    ADD COLUMN request_key uuid,
    ADD CONSTRAINT messages_sender_request_key UNIQUE (sender_id, request_key);
