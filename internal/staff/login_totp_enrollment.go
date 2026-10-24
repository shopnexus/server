package staff

import (
	"context"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/member"
)

type EnrollmentRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// EnrollTOTP gives a member without TOTP a fresh secret, which the first
// correct code at Login confirms; a second call replaces it. The password
// is checked and charged as in Login. An enrolled member is refused, so a
// stolen password cannot replace the authenticator; an admin resets it.
func EnrollTOTP(ctx context.Context, members member.Store, locks LockStore, k Keys, email, password string, now time.Time) (Enrollment, error) {
	m, key, err := checkPassword(ctx, members, locks, k, email, password, now)
	if err != nil {
		return Enrollment{}, err
	}
	if m.Next() == member.StepChangePassword {
		return Enrollment{}, refund(ctx, locks, key, now, ErrPasswordChangeRequired)
	}
	if m.Enrolled() {
		return Enrollment{}, refund(ctx, locks, key, now, ErrTOTPEnrolled)
	}
	secret := member.NewTOTPSecret()
	m.StartEnrollment(secret, now)
	if err = members.Save(ctx, &m, uuid.NewV7()); err != nil {
		return Enrollment{}, saveFailed(err)
	}
	if err = refund(ctx, locks, key, now, nil); err != nil {
		return Enrollment{}, err
	}
	return Enrollment{Secret: member.EncodeSecret(secret), OTPAuthURI: member.TOTPURI(secret, m.Email())}, nil
}

// EnrollTOTPHandler serves POST /v1/staff/login/totp-enrollment.
func EnrollTOTPHandler(st Stores, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in EnrollmentRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		enr, err := EnrollTOTP(r.Context(), st.Members, st.Locks, k, in.Email, in.Password, now())
		if err != nil {
			logRefusal(r, log, in.Email, err)
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, enr)
	})
}
