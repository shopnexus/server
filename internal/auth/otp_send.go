package auth

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
)

var (
	ErrOTPTooSoon = apperr.New(apperr.KindRateLimited, "otp_too_soon", "wait before requesting another code")
	ErrOTPLimit   = apperr.New(apperr.KindRateLimited, "otp_limit", "too many codes requested; try again later")
)

var (
	// resend refuses with ErrOTPTooSoon; every other rule with ErrOTPLimit.
	resend           = otp.Rule{Window: 45 * time.Second, Max: 1}
	destinationRules = []otp.Rule{resend, {Window: time.Hour, Max: 5}}
	addressRules     = []otp.Rule{{Window: time.Hour, Max: 20}}
)

// AdmitOTP spends one send from the client address's quota, then the
// destination's. The address goes first, so an address over its limit cannot
// spend the quota of the numbers it names. stepWrite is the step's journaled
// write ID: a retried step finds it on the quotas it already saved and does
// not count the send again.
func AdmitOTP(ctx context.Context, quotas otp.QuotaStore, dest contact.Address, ip netip.Addr, now time.Time, stepWrite uuid.UUID) error {
	if err := admit(ctx, quotas, addressKey(ip), now, addressRules, stepWrite); err != nil {
		return err
	}
	return admit(ctx, quotas, destinationKey(dest), now, destinationRules, stepWrite)
}

func destinationKey(dest contact.Address) string { return "dest:" + dest.Value() }

// addressKey limits an IPv6 client by its /64, the block one host usually
// holds.
func addressKey(ip netip.Addr) string {
	if ip.Is4() {
		return "ip:" + ip.String()
	}
	p, _ := ip.Prefix(64)
	return "ip:" + p.String()
}

// admit retries a lost race three times, so parallel sends behind one NAT do
// not get a spurious 429. A lost race writes nothing, so the retry reuses
// writeID.
func admit(ctx context.Context, quotas otp.QuotaStore, key string, now time.Time, rules []otp.Rule, writeID uuid.UUID) error {
	for range 3 {
		q, err := quotas.Get(ctx, key)
		if err != nil {
			return err
		}
		if q.WriteID() == writeID {
			return nil
		}
		rule, wait, ok := q.Admit(now, rules...)
		if !ok {
			if rule == resend {
				return ErrOTPTooSoon.WithDetail("retry_after", int(math.Ceil(wait.Seconds())))
			}
			return ErrOTPLimit
		}
		if err := quotas.Save(ctx, &q, writeID); !errors.Is(err, otp.ErrConflict) {
			return err
		}
	}
	return ErrOTPLimit
}

// NewChallenge builds the challenge for id. Its code is derived from id, so
// the send step computes the same code again.
func NewChallenge(codes otp.Codes, id uuid.UUID, purpose otp.Purpose, dest contact.Address, ip netip.Addr, now time.Time) otp.Challenge {
	return otp.New(id, purpose, dest, ip, codes.Hash(dest, codes.For(id)), now)
}

// SendCode sends a challenge's code and reports whether it did: a challenge
// that has expired by now is not sent.
func SendCode(ctx context.Context, smsSender sms.Sender, emailSender email.Sender, codes otp.Codes, c otp.Challenge, now time.Time) (bool, error) {
	if !now.Before(c.ExpiresAt()) {
		return false, nil
	}
	text := "Mã xác thực shopnexus: " + codes.For(c.ID()) + ". Mã hết hạn sau 5 phút. Không chia sẻ mã này với ai."
	if c.Destination().Kind() == contact.Email {
		return true, emailSender.Send(ctx, c.Destination().Value(), "Mã xác thực shopnexus", text)
	}
	return true, smsSender.Send(ctx, c.Destination().Value(), text)
}

type OTPSendRequest struct {
	Kind        contact.Kind `json:"kind"`
	Destination string       `json:"destination"`
	Purpose     otp.Purpose  `json:"purpose"`
}

type OTPSendResponse struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	ResendAfter int       `json:"resend_after"`
}

// POST /v1/auth/otp
func OTPSendHandler(rs Restate, trusted []netip.Prefix, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		in, err := httpx.Decode[OTPSendRequest](r)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		dest, err := contact.Parse(in.Kind, in.Destination)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		ip := httpx.ClientIP(r, trusted)
		if !ip.IsValid() {
			httpx.WriteError(w, r, log, errors.New("otp send: no client address"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), restateTimeout)
		defer cancel()
		res, err := rs.OTP().Request(ctx,
			OTPSendInput{Kind: dest.Kind(), Destination: dest.Value(), Purpose: in.Purpose, IP: ip},
			restate.WithIdempotencyKey(r.Header.Get("Idempotency-Key")))
		if err == nil {
			err = res.Refusal.Err()
		}
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, r, http.StatusOK, OTPSendResponse{ChallengeID: res.ChallengeID, ResendAfter: int(resend.Window.Seconds())})
	})
}
