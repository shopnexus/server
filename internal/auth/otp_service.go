package auth

import (
	"errors"
	"math"
	"net/netip"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
)

type OTPSendInput struct {
	Kind        contact.Kind `json:"kind"`
	Destination string       `json:"destination"` // normalised by the handler
	Purpose     otp.Purpose  `json:"purpose"`
	IP          netip.Addr   `json:"ip"`
}

// OTPSendResult carries a refusal as data: Restate retries an error, and its
// ingress keeps only a message of a terminal one.
type OTPSendResult struct {
	ChallengeID uuid.UUID     `json:"challenge_id"`
	Refused     *apperr.Error `json:"refused,omitempty"`
}

// OTP is the Restate service that sends codes.
type OTP struct {
	Prefix     string
	Quotas     otp.QuotaStore
	Challenges otp.ChallengeStore
	Codes      otp.Codes
	SMS        sms.Sender
	Email      email.Sender
	Now        func() time.Time
}

func (s OTP) ServiceName() string { return s.Prefix + "OTP" }

func (s OTP) Send(ctx restate.Context, in OTPSendInput) (OTPSendResult, error) {
	dest := contact.Restore(in.Kind, in.Destination)
	at, err := clock(ctx, s.Now)
	if err != nil {
		return OTPSendResult{}, err
	}

	// The address goes first, so an address over its limit cannot spend the
	// quota of the numbers it names. A quota that admitted the send stays
	// spent even when a later step fails.
	if refused, err := admit(ctx, s.Quotas, addressKey(in.IP), at, addressRules); err != nil || refused != nil {
		return OTPSendResult{Refused: refused}, err
	}
	if refused, err := admit(ctx, s.Quotas, destinationKey(dest), at, destinationRules); err != nil || refused != nil {
		return OTPSendResult{Refused: refused}, err
	}

	c := NewChallenge(s.Codes, newID(ctx, at), in.Purpose, dest, in.IP, at)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Challenges.Create(rc, c) }); err != nil {
		return OTPSendResult{}, err
	}
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		sent, err := SendCode(rc, s.SMS, s.Email, s.Codes, c, s.Now())
		if sent && err == nil {
			otpSent.Add(rc, 1, metric.WithAttributes(attribute.String("kind", string(dest.Kind()))))
		}
		return err
	}); err != nil {
		return OTPSendResult{}, err
	}
	return OTPSendResult{ChallengeID: c.ID()}, nil
}

// admit spends one send from the quota at key. It reads the quota in one step,
// decides between steps and saves in another, so a retried save does not
// count the send twice. It retries a lost race three times, so parallel sends
// behind one NAT do not get a spurious 429.
func admit(ctx restate.Context, quotas otp.QuotaStore, key string, at time.Time, rules []otp.Rule) (*apperr.Error, error) {
	for range 3 {
		q, err := restate.Run(ctx, func(rc restate.RunContext) (otp.Quota, error) { return quotas.Get(rc, key) })
		if err != nil {
			return nil, err
		}
		rule, wait, ok := q.Admit(at, rules...)
		if !ok {
			refused := ErrOTPLimit
			if rule == resend {
				refused = ErrOTPTooSoon
			}
			return refused.WithDetail("retry_after", int(math.Ceil(wait.Seconds()))), nil
		}
		w := newID(ctx, at)
		lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
			q := q // Save sets the write ID on its copy; q outside stays as journaled
			err := quotas.Save(rc, &q, w)
			return errors.Is(err, otp.ErrConflict), ignore(err, otp.ErrConflict)
		})
		if err != nil || !lost {
			return nil, err
		}
	}
	// The racing sends finish within a second.
	return ErrOTPLimit.WithDetail("retry_after", 1), nil
}
