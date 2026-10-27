package domain

import "github.com/shopnexus/server2/internal/platform/apperr"

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
)
