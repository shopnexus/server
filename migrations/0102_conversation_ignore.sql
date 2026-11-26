-- A member who ignores a conversation stops hearing of it: its messages
-- list under Đã bỏ qua, count as unread nowhere and push nothing.
ALTER TABLE conversation_members ADD COLUMN ignored_at timestamptz;
