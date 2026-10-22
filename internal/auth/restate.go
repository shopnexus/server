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
	"fmt"
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
	return ingress.Service[OTPSendInput, OTPSendResult](r.Client, r.Prefix+otpService, "Send")
}

func (r Restate) SignIn() ingress.Requester[SignInInput, SignInResult] {
	return ingress.Service[SignInInput, SignInResult](r.Client, r.Prefix+signInService, "Run")
}

// clock returns now() from a journaled step, so a replay sees the same time.
func clock(ctx restate.Context, now func() time.Time) (time.Time, error) {
	t, err := restate.Run(ctx, func(restate.RunContext) (time.Time, error) { return now(), nil })
	if err != nil {
		return time.Time{}, fmt.Errorf("read clock: %w", err)
	}
	return t, nil
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
	r := restate.Rand(ctx)
	var b [24]byte
	binary.BigEndian.PutUint64(b[0:8], uint64(at.UnixMilli()))
	binary.BigEndian.PutUint64(b[8:16], r.Uint64())
	binary.BigEndian.PutUint64(b[16:24], r.Uint64())
	// 48-bit millisecond time, then 80 random bits: all of the first draw and
	// the low two bytes of the second.
	var id uuid.UUID
	copy(id[0:6], b[2:8])
	copy(id[6:14], b[8:16])
	copy(id[14:16], b[22:24])
	id[6] = 0x70 | id[6]&0x0f
	id[8] = 0x80 | id[8]&0x3f
	return id
}
