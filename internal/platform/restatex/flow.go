package restatex

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
)

// CallTimeout bounds how long an HTTP handler waits for a service. The
// invocation goes on after a timeout, and the app's retry with the same
// Idempotency-Key picks up its result.
const CallTimeout = 15 * time.Second

// Now returns now() from a journaled step, so a replay sees the same time.
// A service reads it once per invocation.
func Now(ctx restate.Context, now func() time.Time) (time.Time, error) {
	t, err := restate.Run(ctx, func(restate.RunContext) (time.Time, error) { return now(), nil })
	if err != nil {
		return time.Time{}, fmt.Errorf("read clock: %w", err)
	}
	return t, nil
}

// NewID returns a UUIDv7 for at. Its random bits come from the invocation's
// deterministic generator, so a replay mints the same ID, and IDs still sort
// by creation time as the spec's cursors assume. restate.UUID gives version 4.
func NewID(ctx restate.Context, at time.Time) uuid.UUID {
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

// Outcome turns target into data for a step to return: (true, nil) when err
// is target, (false, err) otherwise. A step uses it for an outcome the
// service decides on, such as a lost race.
func Outcome(err, target error) (bool, error) {
	if errors.Is(err, target) {
		return true, nil
	}
	return false, err
}

// Ignore returns err unless it is target. A step uses it for an outcome that
// changes nothing, such as a hint that lost a race.
func Ignore(err, target error) error {
	if errors.Is(err, target) {
		return nil
	}
	return err
}
