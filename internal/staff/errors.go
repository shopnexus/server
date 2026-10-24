package staff

import (
	"math"
	"time"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

var (
	ErrLoginLocked            = apperr.New(apperr.KindRateLimited, "login_locked", "too many failed sign-ins on this email; wait and try again")
	ErrPasswordChangeRequired = apperr.New(apperr.KindPreconditionRequired, "password_change_required", "replace the temporary password: send new_password")
	ErrTOTPEnrollmentRequired = apperr.New(apperr.KindPreconditionRequired, "totp_enrollment_required", "enroll TOTP with /v1/staff/login/totp-enrollment, then send its first code")
	ErrTOTPRequired           = apperr.New(apperr.KindPreconditionRequired, "totp_required", "send the code from the authenticator")
	ErrTOTPEnrolled           = apperr.New(apperr.KindConflict, "totp_already_enrolled", "TOTP is already enrolled; an admin can reset it")
	ErrNotFromConsole         = apperr.New(apperr.KindBadRequest, "invalid_request", "staff calls need X-Requested-With: shopnexus-console")
	ErrStaffForbidden         = apperr.New(apperr.KindForbidden, "staff_forbidden", "your role cannot do this")
	ErrMemberNotFound         = apperr.New(apperr.KindNotFound, "staff_member_not_found", "no staff member has this id")
)

func locked(wait time.Duration) error {
	return ErrLoginLocked.WithDetail("retry_after", int(math.Ceil(wait.Seconds())))
}
