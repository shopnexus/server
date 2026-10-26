-- Blocks: neither person may message the other, open a conversation with
-- them or open a deal on their listings while the block stands.

CREATE TABLE account_blocks (
    id         uuid PRIMARY KEY,
    blocker_id uuid NOT NULL REFERENCES accounts (id),
    blocked_id uuid NOT NULL REFERENCES accounts (id),
    created_at timestamptz NOT NULL,
    UNIQUE (blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id)
);

CREATE INDEX account_blocks_blocked ON account_blocks (blocked_id, blocker_id);
