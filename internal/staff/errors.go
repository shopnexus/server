package staff

import (
	"math"
	"time"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

var (
	ErrLoginLocked        = apperr.New(apperr.KindRateLimited, "login_locked", "too many failed sign-ins on this email; wait and try again")
	ErrInvalidLoginTicket = apperr.New(apperr.KindUnauthenticated, "invalid_login_ticket", "the sign-in ticket is not valid; start again")
	ErrStaffForbidden     = apperr.New(apperr.KindForbidden, "staff_forbidden", "your role cannot do this")
	ErrMemberNotFound     = apperr.New(apperr.KindNotFound, "staff_member_not_found", "no staff member has this id")
)

func locked(wait time.Duration) error {
	return ErrLoginLocked.WithDetail("retry_after", int(math.Ceil(wait.Seconds())))
}
