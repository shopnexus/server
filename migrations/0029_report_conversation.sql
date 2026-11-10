-- A report of an account may name the reporter's conversation with it, so
-- the console can read the messages it was made from.
ALTER TABLE reports ADD COLUMN conversation_id uuid REFERENCES conversations (id);
ALTER TABLE reports ADD CONSTRAINT reports_conversation_account
    CHECK (conversation_id IS NULL OR listing_id IS NULL);
