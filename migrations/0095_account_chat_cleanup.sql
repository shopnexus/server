-- Columns nothing reads: a password never came, a dashboard session lives
-- as long as its console session, and the audit entry names who replied.
ALTER TABLE credentials DROP COLUMN password_hash;
ALTER TABLE console_dashboard_sessions DROP COLUMN expires_at;
ALTER TABLE feedback DROP COLUMN replied_by;

-- The unique (account_id, kind) and (account_id, request_key) serve these.
DROP INDEX identities_account_id;
DROP INDEX sessions_account_id;

-- A deleted message or conversation checks these foreign keys. The tables
-- are small, so a plain CREATE INDEX in the transaction is fine.
CREATE INDEX messages_reply_to ON messages (reply_to) WHERE reply_to IS NOT NULL;
CREATE INDEX reports_conversation ON reports (conversation_id) WHERE conversation_id IS NOT NULL;
CREATE INDEX tickets_first_message ON tickets (first_message_id) WHERE first_message_id IS NOT NULL;

-- A sent message trims the reply samples past 30 days, and a new notice
-- the notices past 90, whoever's they are.
CREATE INDEX reply_samples_answered ON reply_samples (answered_at);
CREATE INDEX notifications_bumped ON notifications (bumped_at);
