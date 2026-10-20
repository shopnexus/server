-- Who a person is. How they sign in lives in the auth tables.
CREATE TABLE accounts (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    display_name    text CHECK (char_length(display_name) BETWEEN 1 AND 50),
    status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    suspended_until timestamptz,
    suspend_reason  text,
    -- The ID of the last write. Save matches it to detect a concurrent
    -- change, and a retried Save with the same ID matches again.
    write_id        uuid NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (status = 'suspended' OR (suspended_until IS NULL AND suspend_reason IS NULL))
);
