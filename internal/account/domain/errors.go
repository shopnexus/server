package domain

import (
	"errors"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

var (
	ErrAccountNotFound = apperr.New(apperr.KindNotFound, "account_not_found", "account not found")
	ErrSuspended       = apperr.New(apperr.KindForbidden, "account_suspended", "account is suspended")
	ErrAccountDeleted  = apperr.New(apperr.KindConflict, "account_deleted", "the other account was deleted")
	ErrInvalidAccount  = apperr.New(apperr.KindInvalid, "invalid_account", "the account breaks a rule; see fields")
	ErrSuspendReason   = apperr.New(apperr.KindInvalid, "suspend_reason_required", "a suspension needs a reason")
	ErrSuspendUntil    = apperr.New(apperr.KindInvalid, "suspend_until_in_past", "a suspension cannot end in the past")
	ErrBlocked         = apperr.New(apperr.KindForbidden, "blocked", "a block stands between the two accounts")
	ErrSelfBlock       = apperr.New(apperr.KindInvalid, "invalid_block", "an account cannot block itself")
	ErrSelfFollow      = apperr.New(apperr.KindInvalid, "invalid_follow", "an account cannot follow itself")
	ErrFollowLimit     = apperr.New(apperr.KindConflict, "follow_limit_reached", "at most 1000 follows")
	ErrAddressNotFound = apperr.New(apperr.KindNotFound, "address_not_found", "no such address of the caller's")
	ErrAddressLimit    = apperr.New(apperr.KindConflict, "address_limit_reached", "at most 10 addresses")
	ErrInvalidAddress  = apperr.New(apperr.KindInvalid, "invalid_address", "the address breaks a rule; see fields")

	ErrInvalidCode = apperr.New(apperr.KindInvalid, "invalid_code", "the code does not match")
	ErrCodeExpired = apperr.New(apperr.KindInvalid, "code_expired", "the code has expired or was replaced; request a new one")
	ErrCodeLocked  = apperr.New(apperr.KindInvalid, "code_locked", "too many wrong codes; request a new one")
	ErrOTPTooSoon  = apperr.New(apperr.KindRateLimited, "otp_too_soon", "wait before requesting another code")
	ErrOTPLimit    = apperr.New(apperr.KindRateLimited, "otp_limit", "too many codes requested; try again later")

	ErrIdentityTaken     = apperr.New(apperr.KindConflict, "identity_taken", "this sign-in method belongs to another account")
	ErrIdentityKindTaken = apperr.New(apperr.KindConflict, "identity_kind_taken", "the account has a sign-in method of this kind already")
	ErrUnknownIdentity   = errors.New("credentials: no such identity")

	ErrInvalidRefreshToken = apperr.New(apperr.KindUnauthenticated, "invalid_refresh_token", "the refresh token is not valid")
	ErrSessionRevoked      = apperr.New(apperr.KindUnauthenticated, "session_revoked", "the session was signed out")
	ErrSessionExpired      = apperr.New(apperr.KindUnauthenticated, "session_expired", "the session has expired; sign in again")
	ErrInvalidToken        = apperr.New(apperr.KindUnauthenticated, "invalid_token", "the access token is not valid")
	ErrReauthRequired      = apperr.New(apperr.KindPreconditionRequired, "reauth_required", "sign in again to do this")
	// ErrSessionNotFound also answers for another account's session: a
	// resource the caller may not see is 404, never 403.
	ErrSessionNotFound = apperr.New(apperr.KindNotFound, "session_not_found", "no such session")
)
