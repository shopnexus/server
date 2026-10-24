-- Staff: the moderators and admins of the web console, their sessions, the
-- sign-in lock per email, and the audit log of what staff did. Staff never
-- share a row with customers.

CREATE TABLE staff_members (
    id                   uuid PRIMARY KEY,
    -- Stored lower-cased, so the unique index is case-insensitive.
    email                text NOT NULL CHECK (email = lower(email) AND char_length(email) <= 254),
    display_name         text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 100),
    role                 text NOT NULL CHECK (role IN ('moderator', 'admin')),
    status               text NOT NULL CHECK (status IN ('active', 'disabled')),
    -- argon2id in PHC string form; the parameters travel with the hash.
    password_hash        text NOT NULL,
    must_change_password boolean NOT NULL,
    -- Raised by a password change or reset, a TOTP reset and a disable. A
    -- session carries the version it was issued at.
    credentials_version  integer NOT NULL,
    -- AES-256-GCM under a key derived from STAFF_KEY, bound to the row's id.
    totp_secret          bytea,
    totp_pending_secret  bytea,
    totp_last_step       bigint NOT NULL,
    created_at           timestamptz NOT NULL,
    updated_at           timestamptz NOT NULL,
    write_id             uuid NOT NULL
);
CREATE UNIQUE INDEX staff_members_email ON staff_members (email);

-- A session's cookie holds a random token; the row holds its SHA-256.
CREATE TABLE staff_sessions (
    id                  uuid PRIMARY KEY,
    staff_id            uuid NOT NULL REFERENCES staff_members,
    token_hash          bytea NOT NULL UNIQUE,
    credentials_version integer NOT NULL,
    ip                  inet NOT NULL,
    user_agent          text NOT NULL CHECK (char_length(user_agent) <= 500),
    created_at          timestamptz NOT NULL,
    expires_at          timestamptz NOT NULL,
    revoked_at          timestamptz
);
CREATE INDEX staff_sessions_staff_id ON staff_sessions (staff_id);

-- Failed sign-ins per email. key is HMAC-SHA256 of the lower-cased email
-- under a key derived from STAFF_KEY, so the table holds no address anyone
-- typed. Unknown emails get rows too, which keeps a lock from telling which
-- emails are staff.
CREATE TABLE staff_login_locks (
    key          bytea PRIMARY KEY,
    failures     integer NOT NULL,
    locked_until timestamptz,
    updated_at   timestamptz NOT NULL
);

-- Append only: no code updates or deletes a row.
CREATE TABLE staff_actions (
    id          uuid PRIMARY KEY,
    actor_kind  text NOT NULL CHECK (actor_kind IN ('staff', 'cli')),
    staff_id    uuid REFERENCES staff_members,
    -- The OS user for the CLI.
    actor_name  text,
    session_id  uuid REFERENCES staff_sessions,
    ip          inet,
    action      text NOT NULL,
    target_kind text NOT NULL CHECK (target_kind IN ('account', 'staff')),
    target_id   uuid NOT NULL,
    details     jsonb NOT NULL,
    at          timestamptz NOT NULL,
    CHECK ((actor_kind = 'staff') = (staff_id IS NOT NULL)),
    CHECK ((actor_kind = 'cli') = (actor_name IS NOT NULL))
);
CREATE INDEX staff_actions_staff_id ON staff_actions (staff_id, id);
CREATE INDEX staff_actions_target_id ON staff_actions (target_id, id);
