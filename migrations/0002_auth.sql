-- How a person signs in. Foreign keys point at accounts and never the other
-- way. Every aggregate root carries write_id, the ID of its last write.

-- Aggregate root for an account's sign-in methods; password_hash arrives
-- with plan 3.
CREATE TABLE credentials (
    account_id    uuid PRIMARY KEY REFERENCES accounts,
    password_hash text,
    write_id      uuid NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- A verified phone, email or Google account, owned by one credentials row.
CREATE TABLE identities (
    id           uuid PRIMARY KEY,
    account_id   uuid NOT NULL REFERENCES credentials ON DELETE CASCADE,
    kind         text NOT NULL CHECK (kind IN ('phone', 'email', 'google')),
    value        text NOT NULL,
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, value)
);
CREATE INDEX identities_account_id ON identities (account_id);

CREATE TABLE sessions (
    id                uuid PRIMARY KEY,
    account_id        uuid NOT NULL REFERENCES accounts,
    refresh_hash      bytea NOT NULL UNIQUE,
    prev_refresh_hash bytea,
    device_name       text CHECK (char_length(device_name) <= 100),
    -- When the person last proved who they are; refresh copies it into each
    -- access token.
    auth_time         timestamptz NOT NULL,
    created_at        timestamptz NOT NULL,
    last_used_at      timestamptz NOT NULL,
    expires_at        timestamptz NOT NULL,
    revoked_at        timestamptz,
    write_id          uuid NOT NULL
);
CREATE INDEX sessions_account_id ON sessions (account_id);
CREATE INDEX sessions_prev_refresh_hash ON sessions (prev_refresh_hash) WHERE prev_refresh_hash IS NOT NULL;

-- code_hash is HMAC-SHA256(OTP_KEY, ...); the key is not in the database.
CREATE TABLE otp_challenges (
    id          uuid PRIMARY KEY,
    purpose     text NOT NULL CHECK (purpose IN ('login', 'add_identity', 'reset_password')),
    kind        text NOT NULL CHECK (kind IN ('phone', 'email')),
    destination text NOT NULL,
    account_id  uuid REFERENCES accounts,
    code_hash   bytea NOT NULL,
    attempts    int NOT NULL DEFAULT 0,
    ip          inet NOT NULL,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    write_id    uuid NOT NULL
);
CREATE INDEX otp_challenges_destination ON otp_challenges (destination, purpose, created_at);

-- Sends inside the longest window, one row per "dest:..." or "ip:..." key.
CREATE TABLE otp_quotas (
    key      text PRIMARY KEY,
    sent_at  timestamptz[] NOT NULL,
    write_id uuid NOT NULL
);
