package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/monitoring/domain"
)

func TestDurations(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]time.Duration{"90s": 90 * time.Second, "15m": 15 * time.Minute, "6h": 6 * time.Hour, "1d": 24 * time.Hour} {
		if got, err := domain.ParseDuration(in); err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0m", "15", "1w", "-1h", "1.5h"} {
		if _, err := domain.ParseDuration(in); err == nil {
			t.Errorf("%q parsed", in)
		}
	}
	for d, want := range map[time.Duration]string{15 * time.Minute: "15m", 2 * time.Hour: "2h", 24 * time.Hour: "1d", 90 * time.Second: "90s"} {
		if got := domain.FormatDuration(d); got != want {
			t.Errorf("%s: %s", d, got)
		}
	}
}

// A range is at most a day, and its step at least 15s and range/250, a
// smaller step raised to it. UC-MOD-13 E1.
func TestSpan(t *testing.T) {
	t.Parallel()
	end := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	if s, err := domain.NewSpan(end, 0, time.Minute); err != nil || s.Step != 0 || s.Start() != end {
		t.Errorf("one time: %+v %v", s, err)
	}
	if s, _ := domain.NewSpan(end, time.Hour, 0); s.Step != 15*time.Second {
		t.Errorf("an hour steps %s", s.Step)
	}
	if s, _ := domain.NewSpan(end, 24*time.Hour, time.Second); s.Step != 346*time.Second || s.Start() != end.Add(-24*time.Hour) {
		t.Errorf("a day steps %s", s.Step)
	}
	if s, _ := domain.NewSpan(end, time.Hour, 5*time.Minute); s.Step != 5*time.Minute {
		t.Errorf("a wider step kept: %s", s.Step)
	}
	if _, err := domain.NewSpan(end, 25*time.Hour, 0); !errors.Is(err, domain.ErrQueryInvalid) {
		t.Errorf("25h: %v", err)
	}
}

// What a log line shows of personal data and secrets. NFR-PRIV-02.
func TestMask(t *testing.T) {
	t.Parallel()
	in := `sent to lan.nguyen@example.vn and 0912345678 (+84987654321), Authorization: Bearer abc.def, ` +
		`token=s3cr3t&x=1 code=123456 snt_abcdefghijklmnopqrstuv eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig ` +
		`trace 01ea5e92866e4c9a2bfafe77649e6849 visitor g:4f1c2d3e4a5b at 1795770070864118987 took 0.966 ms`
	want := `sent to <email> and <phone> (<phone>), Authorization: Bearer <token>, ` +
		`token=<masked>&x=1 code=<masked> <token> <token> ` +
		`trace 01ea5e92866e4c9a2bfafe77649e6849 visitor g:4f1c2d3e4a5b at 1795770070864118987 took 0.966 ms`
	if got := domain.Mask(in); got != want {
		t.Errorf("masked\n%s\nwant\n%s", got, want)
	}
	if got := domain.Mask(strings.Repeat("ả", 3000)); len([]rune(got)) != domain.LineCut+1 {
		t.Errorf("a long line is %d runes", len([]rune(got)))
	}
}
