package domain

import (
	"math"
	"time"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

var (
	ErrEmailTaken         = apperr.New(apperr.KindConflict, "staff_email_taken", "a staff member already has this email")
	ErrInvalidMember      = apperr.New(apperr.KindInvalid, "invalid_staff_member", "a staff member needs a valid email, a display name of 1 to 100 characters and a known role")
	ErrInvalidCredentials = apperr.New(apperr.KindUnauthenticated, "invalid_credentials", "the email or password is wrong")
	ErrInvalidTOTPCode    = apperr.New(apperr.KindUnauthenticated, "invalid_totp_code", "the code is wrong or was already used")
	ErrPasswordTooWeak    = apperr.New(apperr.KindInvalid, "password_too_weak", "a password has 12 to 128 characters and is neither the email nor the current password")
	ErrAdminManagedByCLI  = apperr.New(apperr.KindForbidden, "admin_managed_by_cli", "admins are managed with cmd/staff")
)

var (
	ErrLoginLocked            = apperr.New(apperr.KindRateLimited, "login_locked", "too many failed sign-ins on this email; wait and try again")
	ErrPasswordChangeRequired = apperr.New(apperr.KindPreconditionRequired, "password_change_required", "replace the temporary password: send new_password")
	ErrTOTPEnrollmentRequired = apperr.New(apperr.KindPreconditionRequired, "totp_enrollment_required", "enroll TOTP with /v1/staff/login/totp-enrollment, then send its first code")
	ErrTOTPRequired           = apperr.New(apperr.KindPreconditionRequired, "totp_required", "send the code from the authenticator")
	ErrTOTPEnrolled           = apperr.New(apperr.KindConflict, "totp_already_enrolled", "TOTP is already enrolled; an admin can reset it")
	ErrStaffForbidden         = apperr.New(apperr.KindForbidden, "staff_forbidden", "your role cannot do this")
	ErrMemberNotFound         = apperr.New(apperr.KindNotFound, "staff_member_not_found", "no staff member has this id")
)

// Locked is ErrLoginLocked with the seconds to wait.
func Locked(wait time.Duration) error {
	return ErrLoginLocked.WithDetail("retry_after", int(math.Ceil(wait.Seconds())))
}
