-- What a person is told: one row per notice, collapsing on a key while
-- unread. params holds what the text is written from; event_id is the event
-- the notice came from, so a redelivered event writes nothing more.
CREATE TABLE notifications (
    id           uuid PRIMARY KEY,
    account_id   uuid NOT NULL REFERENCES accounts (id),
    notice_type  text NOT NULL,
    collapse_key text,
    params       jsonb NOT NULL,
    event_id     uuid NOT NULL,
    read_at      timestamptz,
    created_at   timestamptz NOT NULL,
    bumped_at    timestamptz NOT NULL
);
CREATE UNIQUE INDEX notifications_unread_key ON notifications (account_id, collapse_key)
WHERE read_at IS NULL AND collapse_key IS NOT NULL;
CREATE INDEX notifications_list ON notifications (account_id, bumped_at DESC, id DESC);
CREATE INDEX notifications_event ON notifications (account_id, event_id);

-- One FCM token per sign-in session.
CREATE TABLE push_tokens (
    session_id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts (id),
    token      text NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX push_tokens_account ON push_tokens (account_id);
