-- Sign-in by email returns: an account holds a phone, an email or both, one
-- of each kind. Google proves an email and is no kind of its own.
ALTER TABLE identities
DROP CONSTRAINT identities_kind_check,
ADD CONSTRAINT identities_kind_check CHECK (kind IN ('phone', 'email')),
ADD CONSTRAINT identities_account_kind UNIQUE (account_id, kind);

-- Codes go to email addresses only; phones go through Firebase. code_hash is
-- HMAC-SHA256(OTP_KEY, ...); the key is not in the database. An add_identity
-- challenge names the account that asked for it, a sign_in one none.
CREATE TABLE otp_challenges (
    id          uuid PRIMARY KEY,
    purpose     text NOT NULL CHECK (purpose IN ('sign_in', 'add_identity')),
    kind        text NOT NULL CHECK (kind = 'email'),
    destination text NOT NULL,
    account_id  uuid REFERENCES accounts,
    code_hash   bytea NOT NULL,
    attempts    int NOT NULL DEFAULT 0,
    ip          inet NOT NULL,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    write_id    uuid NOT NULL,
    CHECK ((purpose = 'add_identity') = (account_id IS NOT NULL))
);
CREATE INDEX otp_challenges_destination ON otp_challenges (destination, purpose, created_at);

-- Sends inside the longest window, one row per subject, "dest:..." or "ip:...".
CREATE TABLE otp_quotas (
    subject  text PRIMARY KEY,
    sent_at  timestamptz[] NOT NULL,
    write_id uuid NOT NULL
);
