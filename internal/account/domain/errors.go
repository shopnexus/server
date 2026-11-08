package domain

import (
	"errors"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/token"
)

var (
	ErrAccountNotFound = apperr.New(apperr.KindNotFound, "account_not_found", "account not found")
	ErrSuspended       = apperr.New(apperr.KindForbidden, "account_suspended", "account is suspended")
	ErrInvalidAccount  = apperr.New(apperr.KindInvalid, "invalid_account", "the account breaks a rule; see fields")
	ErrSuspendReason   = apperr.New(apperr.KindInvalid, "suspend_reason_required", "a suspension needs a reason")
	ErrSuspendUntil    = apperr.New(apperr.KindInvalid, "suspend_until_in_past", "a suspension cannot end in the past")
	ErrBlocked         = apperr.New(apperr.KindForbidden, "blocked", "a block stands between the two accounts")
	ErrSelfBlock       = apperr.New(apperr.KindInvalid, "invalid_block", "an account cannot block itself")
	ErrAddressNotFound = apperr.New(apperr.KindNotFound, "address_not_found", "no such address of the caller's")
	ErrAddressLimit    = apperr.New(apperr.KindConflict, "address_limit_reached", "at most 10 addresses")
	ErrInvalidAddress  = apperr.New(apperr.KindInvalid, "invalid_address", "the address breaks a rule; see fields")

	ErrIdentityTaken   = apperr.New(apperr.KindConflict, "identity_taken", "this sign-in method belongs to another account")
	ErrUnknownIdentity = errors.New("credentials: no such identity")

	ErrInvalidRefreshToken = apperr.New(apperr.KindUnauthenticated, "invalid_refresh_token", "the refresh token is not valid")
	ErrSessionRevoked      = apperr.New(apperr.KindUnauthenticated, "session_revoked", "the session was signed out")
	ErrSessionExpired      = apperr.New(apperr.KindUnauthenticated, "session_expired", "the session has expired; sign in again")
	ErrInvalidToken        = token.ErrInvalid
	ErrReauthRequired      = apperr.New(apperr.KindPreconditionRequired, "reauth_required", "sign in again to do this")
	// ErrSessionNotFound also answers for another account's session: a
	// resource the caller may not see is 404, never 403.
	ErrSessionNotFound = apperr.New(apperr.KindNotFound, "session_not_found", "no such session")
)
