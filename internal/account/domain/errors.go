package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

var (
	ErrAccountNotFound = apperr.New(apperr.KindNotFound, "account_not_found", "account not found")
	ErrSuspended       = apperr.New(apperr.KindForbidden, "account_suspended", "account is suspended")
	ErrInvalidName     = apperr.New(apperr.KindInvalid, "invalid_display_name", "display name must be 1 to 50 characters")
	ErrSuspendReason   = apperr.New(apperr.KindInvalid, "suspend_reason_required", "a suspension needs a reason")
	ErrSuspendUntil    = apperr.New(apperr.KindInvalid, "suspend_until_in_past", "a suspension cannot end in the past")
)
