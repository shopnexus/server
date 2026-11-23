package apperr_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/platform/apperr"
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
	err := fmt.Errorf("load: %w", sentinel.WithField("id", apperr.Unknown, "unknown"))
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
	a := base.WithField("x", apperr.Required, "missing")
	if base.Fields != nil || base.FieldCodes != nil {
		t.Fatal("WithField mutated the receiver")
	}
	if a.Fields["x"] != "missing" || a.FieldCodes["x"] != apperr.Required {
		t.Fatalf("fields %v codes %v", a.Fields, a.FieldCodes)
	}
	b := a.WithField("y", apperr.TooShort, "short")
	if len(a.Fields) != 1 || len(b.Fields) != 2 || len(a.FieldCodes) != 1 || len(b.FieldCodes) != 2 {
		t.Fatalf("a %v b %v", a.FieldCodes, b.FieldCodes)
	}
}

func TestDetailsCopy(t *testing.T) {
	t.Parallel()
	base := apperr.New(apperr.KindInvalid, "invalid_code", "the code does not match")
	got := base.WithAttemptsLeft(4)
	if got.Details.AttemptsLeft == nil || *got.Details.AttemptsLeft != 4 || !reflect.ValueOf(base.Details).IsZero() || !errors.Is(got, base) {
		t.Fatalf("copy %+v, base %+v", got.Details, base.Details)
	}
	again := got.WithReason("")
	if got.Details.Reason != nil || again.Details.Reason == nil || *again.Details.Reason != "" || again.Details.AttemptsLeft != got.Details.AttemptsLeft {
		t.Fatalf("got %+v, again %+v", got.Details, again.Details)
	}
}

func TestWithCauseCopies(t *testing.T) {
	t.Parallel()
	cause := errors.New("db down")
	base := apperr.New(apperr.KindInternal, "internal", "x").WithField("id", apperr.Unknown, "unknown")
	got := base.WithCause(cause)
	if base.Err != nil || !errors.Is(got, cause) || !errors.Is(got, base) || got.Fields["id"] != "unknown" {
		t.Fatalf("got %+v, base %+v", got, base)
	}
}

func TestWithKindCopies(t *testing.T) {
	t.Parallel()
	base := apperr.New(apperr.KindUnauthenticated, "invalid_phone_token", "x")
	got := base.WithKind(apperr.KindInvalid)
	if base.Kind != apperr.KindUnauthenticated || got.Kind != apperr.KindInvalid || got.Code != base.Code || !errors.Is(got, base) {
		t.Fatalf("got %+v, base %+v", got, base)
	}
}

// A part second rounds up, so a client that waits retry_after is not
// refused again.
func TestWithRetryAfterRoundsUp(t *testing.T) {
	t.Parallel()
	base := apperr.New(apperr.KindRateLimited, "otp_limit", "too many")
	for wait, want := range map[time.Duration]int{time.Second: 1, 1500 * time.Millisecond: 2, 44*time.Second + time.Millisecond: 45} {
		if got := base.WithRetryAfter(wait).Details.RetryAfter; got == nil || *got != want {
			t.Errorf("%s: %v, want %d", wait, got, want)
		}
	}
	if !reflect.ValueOf(base.Details).IsZero() {
		t.Fatalf("the sentinel changed: %+v", base.Details)
	}
}

// A missing spec list is the error's own, and the codes of a length or a
// count tell empty, short and long apart.
func TestMissingSpecsAndLengths(t *testing.T) {
	t.Parallel()
	keys, labels := []string{"color"}, []string{"Màu"}
	got := apperr.New(apperr.KindInvalid, "invalid_listing", "x").WithMissingSpecs(keys, labels)
	keys[0], labels[0] = "changed", "changed"
	if d := got.Details; len(d.MissingSpecs) != 1 || d.MissingSpecs[0] != "Màu" || len(d.MissingSpecKeys) != 1 || d.MissingSpecKeys[0] != "color" {
		t.Fatalf("missing specs %v %v", d.MissingSpecKeys, d.MissingSpecs)
	}
	for _, c := range []struct {
		got, want apperr.FieldCode
	}{
		{apperr.Length(0, 1, 50), apperr.Required},
		{apperr.Length(2, 3, 50), apperr.TooShort},
		{apperr.Length(51, 1, 50), apperr.TooLong},
		{apperr.Length(50, 1, 50), ""},
		{apperr.Length(0, 0, 50), ""},
		{apperr.Count(0, 1, 10), apperr.Required},
		{apperr.Count(11, 1, 10), apperr.TooMany},
		{apperr.Count(1, 2, 10), apperr.TooFew},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
