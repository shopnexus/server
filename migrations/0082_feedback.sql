-- Feedback (2026-11-24): what a person tells ShopNexus without asking for
-- an answer. A group is flat: root_id names the group's root, NULL on the
-- root. Status, note and version are the group's, copied on each row.
CREATE TABLE feedback (
    id              uuid PRIMARY KEY,
    account_id      uuid NOT NULL REFERENCES accounts (id),
    request_key     uuid NOT NULL,
    mood            smallint CHECK (mood BETWEEN 1 AND 5),
    kind            text CHECK (kind IN ('bug', 'idea', 'praise')),
    body            text CHECK (char_length(body) BETWEEN 1 AND 1000),
    photos          jsonb NOT NULL,
    device_model    text CHECK (char_length(device_model) <= 100),
    device_os       text CHECK (char_length(device_os) <= 40),
    app_version     text CHECK (char_length(app_version) <= 40),
    screen          text CHECK (char_length(screen) BETWEEN 1 AND 60),
    contact         boolean NOT NULL,
    status          text NOT NULL CHECK (status IN ('sent', 'seen', 'in_progress', 'shipped')),
    status_note     text CHECK (char_length(status_note) BETWEEN 1 AND 80),
    shipped_version text CHECK (char_length(shipped_version) BETWEEN 1 AND 20),
    status_at       timestamptz NOT NULL,
    root_id         uuid REFERENCES feedback (id),
    reply_body      text CHECK (char_length(reply_body) BETWEEN 1 AND 1000),
    replied_at      timestamptz,
    replied_by      uuid REFERENCES console_members (id),
    hearted         boolean NOT NULL,
    created_at      timestamptz NOT NULL,
    UNIQUE (account_id, request_key),
    CHECK (mood IS NOT NULL OR body IS NOT NULL),
    CHECK ((device_model IS NULL) = (device_os IS NULL) AND (device_os IS NULL) = (app_version IS NULL)),
    CHECK ((reply_body IS NULL) = (replied_at IS NULL)),
    CHECK (shipped_version IS NULL OR status = 'shipped'),
    CHECK (root_id <> id)
);
CREATE INDEX feedback_account ON feedback (account_id, created_at);
CREATE INDEX feedback_root ON feedback (root_id) WHERE root_id IS NOT NULL;
CREATE INDEX feedback_status ON feedback (status, id DESC);

ALTER TABLE console_actions DROP CONSTRAINT console_actions_target_kind_check;
ALTER TABLE console_actions ADD CONSTRAINT console_actions_target_kind_check
CHECK (
    target_kind IN (
        'account', 'member', 'ticket', 'listing', 'ai_provider', 'ai_model', 'ai_agent', 'ai_settings', 'dashboard',
        'feedback'
    )
);
