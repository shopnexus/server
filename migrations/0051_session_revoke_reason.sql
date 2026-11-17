-- Why a session ended: a sign-out, a refresh token two parties presented,
-- a refresh that found the account suspended, or the account's deletion. A
-- session its account's suspension ended answers a refresh with
-- account_suspended for as long as the suspension lasts. Sessions revoked
-- before this file have no reason and answer session_revoked.

ALTER TABLE sessions
ADD COLUMN revoke_reason text CHECK (revoke_reason IN ('sign_out', 'reuse', 'suspended', 'deleted')),
ADD CONSTRAINT sessions_revoke_reason_revoked CHECK (revoke_reason IS NULL OR revoked_at IS NOT NULL);
