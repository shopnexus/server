package member

import (
	"errors"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

var (
	ErrNotFound = errors.New("staff member: not found")
	ErrConflict = errors.New("staff member: changed meanwhile")

	ErrEmailTaken         = apperr.New(apperr.KindConflict, "staff_email_taken", "a staff member already has this email")
	ErrInvalidMember      = apperr.New(apperr.KindInvalid, "invalid_staff_member", "a staff member needs a valid email, a display name of 1 to 100 characters and a known role")
	ErrInvalidCredentials = apperr.New(apperr.KindUnauthenticated, "invalid_credentials", "the email or password is wrong")
	ErrInvalidTOTPCode    = apperr.New(apperr.KindUnauthenticated, "invalid_totp_code", "the code is wrong or was already used")
	ErrPasswordTooWeak    = apperr.New(apperr.KindInvalid, "password_too_weak", "a password has 12 to 128 characters and is neither the email nor the current password")
	ErrAdminManagedByCLI  = apperr.New(apperr.KindForbidden, "admin_managed_by_cli", "admins are managed with cmd/staff")
)
