-- A member marks a conversation unread; it counts as one unread message
-- until the member reads it. The read marker does not move back.
ALTER TABLE conversation_members ADD COLUMN marked_unread boolean NOT NULL DEFAULT false;
