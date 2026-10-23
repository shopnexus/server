package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/restatex"
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
		if err := emailSender.Send(ctx, c.Destination().Value(), "Mã xác thực shopnexus", text); err != nil {
			return true, fmt.Errorf("send code by email: %w", err)
		}
		return true, nil
	}
	if err := smsSender.Send(ctx, c.Destination().Value(), text); err != nil {
		return true, fmt.Errorf("send code by sms: %w", err)
	}
	return true, nil
}

type OTPSendRequest struct {
	IdempotencyKey string       `header:"Idempotency-Key" json:"-"`
	Kind           contact.Kind `json:"kind"`
	Destination    string       `json:"destination"`
	Purpose        otp.Purpose  `json:"purpose"`
}

type OTPSendResponse struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	ResendAfter int       `json:"resend_after"`
}

// OTPSendHandler serves POST /v1/auth/otp.
func OTPSendHandler(rs Restate, trusted []netip.Prefix, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in OTPSendRequest
		if err := httpx.Bind(r, &in); err != nil {
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
		ctx, cancel := context.WithTimeout(r.Context(), restatex.CallTimeout)
		defer cancel()
		res, err := rs.OTP().Request(ctx,
			OTPSendInput{Kind: dest.Kind(), Destination: dest.Value(), Purpose: in.Purpose, IP: ip},
			restate.WithIdempotencyKey(in.IdempotencyKey))
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		if res.Refused != nil {
			httpx.WriteError(w, r, log, res.Refused)
			return
		}
		httpx.Respond(w, http.StatusOK, OTPSendResponse{ChallengeID: res.ChallengeID, ResendAfter: int(resend.Window.Seconds())})
	})
}
