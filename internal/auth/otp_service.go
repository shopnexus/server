package auth

import (
	"net/netip"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/auth/otp"
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

type OTPSendResult struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Refusal     *Refusal  `json:"refusal,omitempty"`
}

// OTP is the Restate service that sends codes. Every step is idempotent: the
// challenge ID is journaled and the code is derived from it.
type OTP struct {
	Prefix     string
	Quotas     otp.QuotaStore
	Challenges otp.ChallengeStore
	Codes      otp.Codes
	SMS        sms.Sender
	Email      email.Sender
}

func (s OTP) ServiceName() string { return s.Prefix + "OTP" }

func (s OTP) Send(ctx restate.Context, in OTPSendInput) (OTPSendResult, error) {
	dest := contact.Restore(in.Kind, in.Destination)
	at, err := clock(ctx)
	if err != nil {
		return OTPSendResult{}, err
	}
	refusal, err := restate.Run(ctx, func(rc restate.RunContext) (*Refusal, error) {
		return refuse(AdmitOTP(rc, s.Quotas, dest, in.IP, at))
	})
	if err != nil || refusal != nil {
		return OTPSendResult{Refusal: refusal}, err
	}
	c := NewChallenge(s.Codes, newID(ctx), in.Purpose, dest, in.IP, at)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Challenges.Create(rc, c) }); err != nil {
		return OTPSendResult{}, err
	}
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		if err := SendCode(rc, s.SMS, s.Email, s.Codes, c, time.Now()); err != nil {
			return err
		}
		otpSent.Add(rc, 1, metric.WithAttributes(attribute.String("kind", string(dest.Kind()))))
		return nil
	}); err != nil {
		return OTPSendResult{}, err
	}
	return OTPSendResult{ChallengeID: c.ID()}, nil
}
