// Package domain holds module chotot's rules: the code that proves a
// profile is the seller's, the profile's address, and what each of the
// profile's listings is on ShopNexus.
package domain

import "github.com/shopnexus/server/internal/platform/apperr"

var (
	ErrInvalidProfileURL = apperr.New(apperr.KindInvalid, "invalid_profile_url",
		"not a Chợ Tốt profile's address, https://www.chotot.com/user/<id>")
	ErrCodeExpired     = apperr.New(apperr.KindInvalid, "code_expired", "no live code; ask for a new one")
	ErrCodeNotFound    = apperr.New(apperr.KindInvalid, "code_not_found", "the profile's bio does not hold the code")
	ErrProfileNotFound = apperr.New(apperr.KindNotFound, "chotot_profile_not_found", "Chợ Tốt has no such profile")
	ErrUnavailable     = apperr.New(apperr.KindUnavailable, "chotot_unavailable", "Chợ Tốt did not answer; try again later")
	ErrNotLinked       = apperr.New(apperr.KindNotFound, "chotot_not_linked", "no Chợ Tốt profile is linked")
	ErrDraftNotFound   = apperr.New(apperr.KindNotFound, "chotot_draft_not_found", "no import of that listing waits as a draft")
)
