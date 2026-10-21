package auth

// The Restate services in this package follow one rule. A restate.Run step
// only reads or writes a store, so any error it returns is a fault Restate
// retries. Every business decision is taken between steps, on values a step
// returned and the journal kept, and code outside a step uses only those
// values. A replay therefore takes the same decisions, and a retried write
// lands the same row: Save accepts a row that already carries its write ID.

import (
	"encoding/binary"
	"errors"
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
	return ingress.Service[OTPSendInput, OTPSendResult](r.Client, OTP{Prefix: r.Prefix}.ServiceName(), "Send")
}

func (r Restate) SignIn() ingress.Requester[SignInInput, SignInResult] {
	return ingress.Service[SignInInput, SignInResult](r.Client, SignIn{Prefix: r.Prefix}.ServiceName(), "Run")
}

// clock returns now() from a journaled step, so a replay sees the same time.
func clock(ctx restate.Context, now func() time.Time) (time.Time, error) {
	return restate.Run(ctx, func(restate.RunContext) (time.Time, error) { return now(), nil })
}

// ignore returns err unless it is target. A step uses it for an outcome it
// reports as data, such as a lost race.
func ignore(err, target error) error {
	if errors.Is(err, target) {
		return nil
	}
	return err
}

// newID returns a UUIDv7 for at. Its random bits come from the invocation's
// deterministic generator, so a replay mints the same ID, and IDs still sort
// by creation time as the spec's cursors assume. restate.UUID gives version 4.
func newID(ctx restate.Context, at time.Time) uuid.UUID {
	var id uuid.UUID
	ms := uint64(at.UnixMilli())
	for i := range 6 {
		id[i] = byte(ms >> (40 - 8*i))
	}
	r := restate.Rand(ctx)
	binary.BigEndian.PutUint64(id[6:14], r.Uint64())
	binary.BigEndian.PutUint16(id[14:16], uint16(r.Uint64()))
	id[6] = 0x70 | id[6]&0x0f
	id[8] = 0x80 | id[8]&0x3f
	return id
}
