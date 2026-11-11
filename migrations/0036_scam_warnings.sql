-- Module trust: scam warnings in chat. A warning is what one member of a
-- conversation is told about the other, the suspect; signals is
-- [{code, params, first_seen_at}] in the order found. A check is one
-- classifier run over a window of a conversation and holds no message text:
-- the window is first_message_id to last_message_id in messages.
CREATE TABLE scam_warnings (
    conversation_id uuid NOT NULL REFERENCES conversations (id),
    account_id      uuid NOT NULL REFERENCES accounts (id),
    suspect_id      uuid NOT NULL REFERENCES accounts (id),
    signals         jsonb NOT NULL,
    dismissed_at    timestamptz,
    updated_at      timestamptz NOT NULL,
    write_id        uuid NOT NULL,
    PRIMARY KEY (conversation_id, account_id),
    CHECK (account_id <> suspect_id)
);

CREATE TABLE scam_checks (
    id               uuid PRIMARY KEY,
    conversation_id  uuid NOT NULL REFERENCES conversations (id),
    first_message_id uuid NOT NULL,
    last_message_id  uuid NOT NULL,
    classifier       text NOT NULL,
    prompt_version   int NOT NULL,
    codes_a          text[] NOT NULL,
    codes_b          text[] NOT NULL,
    created_at       timestamptz NOT NULL
);
-- The plan step reads a conversation's newest check and counts its last day.
CREATE INDEX scam_checks_conversation_created ON scam_checks (conversation_id, created_at DESC);
-- The save step deletes checks past retention, oldest first.
CREATE INDEX scam_checks_created ON scam_checks (created_at);
