-- Chat: one conversation per pair of people, whatever their roles, and its
-- messages. A member reads up to a message and may archive the
-- conversation; the next sent message clears both members' archives.

CREATE TABLE conversations (
    id              uuid PRIMARY KEY,
    account_a       uuid NOT NULL REFERENCES accounts (id),
    account_b       uuid NOT NULL REFERENCES accounts (id),
    -- The newest sent message, which orders the inbox.
    last_message_id uuid,
    last_message_at timestamptz,
    created_at      timestamptz NOT NULL,
    UNIQUE (account_a, account_b),
    CHECK (account_a < account_b)
);

CREATE TABLE conversation_members (
    conversation_id      uuid NOT NULL REFERENCES conversations (id),
    account_id           uuid NOT NULL REFERENCES accounts (id),
    last_read_message_id uuid,
    archived_at          timestamptz,
    PRIMARY KEY (conversation_id, account_id)
);

CREATE INDEX conversation_members_account ON conversation_members (account_id);

CREATE TABLE messages (
    id              uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations (id),
    sender_id       uuid NOT NULL REFERENCES accounts (id),
    body            text NOT NULL CHECK (char_length(body) <= 1000),
    photos          jsonb NOT NULL,
    listing_id      uuid REFERENCES listings (id),
    status          text NOT NULL CHECK (status IN ('processing', 'sent', 'failed')),
    failure         text,
    created_at      timestamptz NOT NULL
);

CREATE INDEX messages_conversation ON messages (conversation_id, id);
