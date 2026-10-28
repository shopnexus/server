package domain

import (
	"errors"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/token"
)

var (
	ErrTaken           = apperr.New(apperr.KindConflict, "identity_taken", "this sign-in method belongs to another account")
	ErrUnknownIdentity = errors.New("credentials: no such identity")

	ErrInvalidRefresh = apperr.New(apperr.KindUnauthenticated, "invalid_refresh_token", "the refresh token is not valid")
	ErrRevoked        = apperr.New(apperr.KindUnauthenticated, "session_revoked", "the session was signed out")
	ErrExpired        = apperr.New(apperr.KindUnauthenticated, "session_expired", "the session has expired; sign in again")
	ErrInvalidToken   = token.ErrInvalid
	ErrReauthRequired = apperr.New(apperr.KindPreconditionRequired, "reauth_required", "sign in again to do this")
	// ErrSessionNotFound also answers for another account's session: a
	// resource the caller may not see is 404, never 403.
	ErrSessionNotFound = apperr.New(apperr.KindNotFound, "session_not_found", "no such session")
)
