package auth

import (
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"
)

// restateTimeout bounds how long a handler waits for a service. The
// invocation goes on after a timeout, and the app's retry with the same
// Idempotency-Key picks up its result.
const restateTimeout = 15 * time.Second

// Restate is how handlers reach auth's Restate services.
type Restate struct {
	Client *ingress.Client
	Prefix string
}

func (r Restate) OTP() ingress.Requester[OTPSendInput, OTPSendResult] {
	return ingress.Service[OTPSendInput, OTPSendResult](r.Client, r.Prefix+"OTP", "Send")
}

// clock returns the time from a journaled step, so a replay sees the same time.
func clock(ctx restate.Context) (time.Time, error) {
	return restate.Run(ctx, func(restate.RunContext) (time.Time, error) { return time.Now(), nil })
}

// newID returns a journaled UUID: the same on every replay.
func newID(ctx restate.Context) uuid.UUID { return uuid.UUID(restate.UUID(ctx)) }
