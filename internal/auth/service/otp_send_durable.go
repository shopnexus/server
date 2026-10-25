package service

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
)

type OTPSendInput struct {
	Kind        contact.Kind   `json:"kind"`
	Destination string         `json:"destination"` // normalised by the handler
	Purpose     domain.Purpose `json:"purpose"`
	IP          netip.Addr     `json:"ip"`
}

// OTPSendResult carries a refusal as data; see restatex.Refusable.
type OTPSendResult struct {
	ChallengeID uuid.UUID     `json:"challenge_id"`
	Refused     *apperr.Error `json:"refused,omitempty"`
}

func (r OTPSendResult) Refusal() *apperr.Error { return r.Refused }

// OTP is the Restate service that sends codes.
type OTP struct {
	Prefix     string
	Quotas     repository.Quotas
	Challenges repository.Challenges
	Codes      domain.Codes
	SMS        sms.Sender
	Email      email.Sender
	Now        func() time.Time
}

// otpService is the OTP service's name without the prefix.
const otpService = "OTP"

func (s OTP) ServiceName() string { return s.Prefix + otpService }

func (s OTP) Send(ctx restate.Context, in OTPSendInput) (OTPSendResult, error) {
	dest := contact.Restore(in.Kind, in.Destination)
	at, err := restatex.Now(ctx, s.Now)
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

	c := NewChallenge(s.Codes, restatex.NewID(ctx, at), in.Purpose, dest, in.IP, at)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Challenges.Create(rc, c) }); err != nil {
		return OTPSendResult{}, fmt.Errorf("create challenge: %w", err)
	}
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		sent, err := SendCode(rc, s.SMS, s.Email, s.Codes, c, s.Now())
		if sent && err == nil {
			otpSent.Add(rc, 1, metric.WithAttributes(attribute.String("kind", string(dest.Kind()))))
		}
		return err
	}); err != nil {
		return OTPSendResult{}, fmt.Errorf("send code: %w", err)
	}
	return OTPSendResult{ChallengeID: c.ID()}, nil
}

// admit spends one send from the quota at key. It reads the quota in one step,
// decides between steps and saves in another, so a retried save does not
// count the send twice. It retries a lost race three times, so parallel sends
// behind one NAT do not get a spurious 429.
func admit(ctx restate.Context, quotas repository.Quotas, key string, at time.Time, rules []domain.Rule) (*apperr.Error, error) {
	for range 3 {
		q, err := restate.Run(ctx, func(rc restate.RunContext) (domain.Quota, error) { return quotas.Get(rc, key) })
		if err != nil {
			return nil, fmt.Errorf("load quota: %w", err)
		}
		rule, wait, ok := q.Admit(at, rules...)
		if !ok {
			refused := domain.ErrOTPLimit
			if rule == resend {
				refused = domain.ErrOTPTooSoon
			}
			return refused.WithDetail("retry_after", int(math.Ceil(wait.Seconds()))), nil
		}
		w := restatex.NewID(ctx, at)
		lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
			q := q // Save sets the write ID on its copy; q outside stays as journaled
			err := quotas.Save(rc, &q, w)
			return restatex.Outcome(err, repository.ErrConflict)
		})
		if err != nil {
			return nil, fmt.Errorf("save quota: %w", err)
		}
		if !lost {
			return nil, nil //nolint:nilnil // A nil refusal means the send was admitted.
		}
	}
	// The racing sends finish within a second.
	return domain.ErrOTPLimit.WithDetail("retry_after", 1), nil
}

var (
	// resend refuses with domain.ErrOTPTooSoon; every other rule with
	// domain.ErrOTPLimit.
	resend           = domain.Rule{Window: 45 * time.Second, Max: 1}
	destinationRules = []domain.Rule{resend, {Window: time.Hour, Max: 5}}
	addressRules     = []domain.Rule{{Window: time.Hour, Max: 20}}
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
func NewChallenge(codes domain.Codes, id uuid.UUID, purpose domain.Purpose, dest contact.Address, ip netip.Addr, now time.Time) domain.Challenge {
	return domain.NewChallenge(id, purpose, dest, ip, codes.Hash(dest, codes.For(id)), now)
}

// SendCode sends a challenge's code and reports whether it did: a challenge
// that has expired by now is not sent.
func SendCode(ctx context.Context, smsSender sms.Sender, emailSender email.Sender, codes domain.Codes, c domain.Challenge, now time.Time) (bool, error) {
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
