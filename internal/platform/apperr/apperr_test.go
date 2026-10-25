package apperr_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

func TestErrorWrapsAndUnwraps(t *testing.T) {
	t.Parallel()
	cause := errors.New("db down")
	e := apperr.New(apperr.KindConflict, "phone_taken", "phone already used")
	e.Err = cause
	wrapped := fmt.Errorf("register: %w", e)

	var got *apperr.Error
	if !errors.As(wrapped, &got) || got.Code != "phone_taken" || got.Kind != apperr.KindConflict {
		t.Fatalf("As: %+v", got)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("cause not reachable through Unwrap")
	}
	if e.Error() != "phone_taken: phone already used: db down" {
		t.Fatalf("Error() = %q", e.Error())
	}
}

func TestErrorsIsComparesCode(t *testing.T) {
	t.Parallel()
	sentinel := apperr.New(apperr.KindNotFound, "account_not_found", "account not found")
	err := fmt.Errorf("load: %w", sentinel.WithField("id", "unknown"))
	if !errors.Is(err, sentinel) {
		t.Fatal("a copy with fields should still match its sentinel")
	}
	if errors.Is(err, apperr.New(apperr.KindNotFound, "other", "x")) {
		t.Fatal("different code matched")
	}
}

func TestWithFieldCopies(t *testing.T) {
	t.Parallel()
	base := apperr.New(apperr.KindInvalid, "invalid", "bad")
	a := base.WithField("x", "missing")
	if base.Fields != nil {
		t.Fatal("WithField mutated the receiver")
	}
	if a.Fields["x"] != "missing" {
		t.Fatalf("fields %v", a.Fields)
	}
	b := a.WithField("y", "short")
	if len(a.Fields) != 1 || len(b.Fields) != 2 {
		t.Fatalf("a %v b %v", a.Fields, b.Fields)
	}
}

func TestWithDetailCopies(t *testing.T) {
	t.Parallel()
	base := apperr.New(apperr.KindInvalid, "invalid_code", "the code does not match")
	got := base.WithDetail("attempts_left", 4)
	if got.Details["attempts_left"] != 4 || base.Details != nil || !errors.Is(got, base) {
		t.Fatalf("copy %v, base %v", got.Details, base.Details)
	}
	if again := got.WithDetail("retry_after", 9); len(got.Details) != 1 || len(again.Details) != 2 {
		t.Fatalf("got %v, again %v", got.Details, again.Details)
	}
}

// A part second rounds up, so a client that waits retry_after is not
// refused again.
func TestWithRetryAfterRoundsUp(t *testing.T) {
	t.Parallel()
	base := apperr.New(apperr.KindRateLimited, "otp_limit", "too many")
	for wait, want := range map[time.Duration]int{time.Second: 1, 1500 * time.Millisecond: 2, 44*time.Second + time.Millisecond: 45} {
		if got := base.WithRetryAfter(wait).Details["retry_after"]; got != want {
			t.Errorf("%s: %v, want %d", wait, got, want)
		}
	}
	if base.Details != nil {
		t.Fatalf("the sentinel changed: %v", base.Details)
	}
}
