// Package domain holds module monitoring's types and rules: what a query
// may ask of the metrics and the logs, and what of a log line reaches the
// one who asked.
package domain

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"

	"github.com/shopnexus/server/internal/platform/apperr"
)

var (
	ErrUnavailable = apperr.New(apperr.KindUnavailable, "monitoring_unavailable",
		"the metrics or the logs cannot be reached, or none are set up")
	ErrQueryInvalid = apperr.New(apperr.KindInvalid, "monitoring_query_invalid",
		"the query does not parse or goes past a limit")
)

// Refused is ErrQueryInvalid with why, the words of the source or of a
// limit, as its message, so the one who wrote the query can fix it.
func Refused(why string) *apperr.Error {
	e := *ErrQueryInvalid
	e.Message = why
	return &e
}

// The limits of a query, so one heavy query does not slow the system
// further while it is in trouble (ADR-0007).
const (
	// MaxRange is how far back a query reaches.
	MaxRange = 24 * time.Hour
	// MinStep is the shortest step of a range query.
	MinStep = 15 * time.Second
	// MaxPoints is the most points a range query's series have.
	MaxPoints = 250
	// MaxSeries is the most series a query returns.
	MaxSeries = 100
	// MaxLines is the most log lines a query returns.
	MaxLines = 500
	// MaxMetrics is the most metrics a list returns.
	MaxMetrics = 100
	// LineCut is where a log line is cut.
	LineCut = 2000
	// QueryTimeout is how long a query may run.
	QueryTimeout = 15 * time.Second
	// WeekBefore is how far back a health figure's comparison lies.
	WeekBefore = 7 * 24 * time.Hour
)

var duration = regexp.MustCompile(`^([1-9]\d{0,4})([smhd])$`)

// ParseDuration reads a Duration of the spec: 15m, 1h, 2d.
func ParseDuration(s string) (time.Duration, error) {
	m := duration.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("%q is not a duration such as 15m", s)
	}
	n, _ := strconv.Atoi(m[1])
	unit := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
	return time.Duration(n) * unit, nil
}

// Duration is a Duration of the spec as a query parameter.
type Duration time.Duration

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := ParseDuration(string(b))
	*d = Duration(v)
	return err
}

// FormatDuration writes d as the largest unit that divides it: 15m, 1h, 90s.
func FormatDuration(d time.Duration) string {
	for _, u := range []struct {
		d    time.Duration
		name string
	}{{24 * time.Hour, "d"}, {time.Hour, "h"}, {time.Minute, "m"}} {
		if d%u.d == 0 {
			return strconv.FormatInt(int64(d/u.d), 10) + u.name
		}
	}
	return strconv.FormatInt(int64(math.Ceil(d.Seconds())), 10) + "s"
}

// Span is when a query looks: a range ending at End, or the one time End
// when Range is zero.
type Span struct {
	End   time.Time
	Range time.Duration
	Step  time.Duration // zero at one time
}

// NewSpan checks a range against MaxRange and sets its step: at least
// MinStep and range/MaxPoints, a smaller step asked for raised to it.
func NewSpan(end time.Time, rng, step time.Duration) (Span, error) {
	if rng > MaxRange {
		return Span{}, ErrQueryInvalid.WithField("range", apperr.OutOfRange, "at most 24h")
	}
	if rng == 0 {
		return Span{End: end}, nil
	}
	// Up to the next whole second, so the points stay within MaxPoints.
	least := max(MinStep, (rng+MaxPoints-1)/MaxPoints)
	least = (least + time.Second - 1).Truncate(time.Second)
	return Span{End: end, Range: rng, Step: max(step, least)}, nil
}

// Start is where a range begins.
func (s Span) Start() time.Time { return s.End.Add(-s.Range) }

// Point is a series' value at a time; Value is nil for NaN or an infinity.
type Point struct {
	At    time.Time `json:"at"`
	Value *float64  `json:"value"`
}

// Series is one labelled series of a query.
type Series struct {
	Labels map[string]string `json:"labels"`
	Points []Point           `json:"points"`
}

// Last is the newest value of a series, nil for none.
func (s Series) Last() *float64 {
	if len(s.Points) == 0 {
		return nil
	}
	return s.Points[len(s.Points)-1].Value
}

// Metric is one metric the system records.
type Metric struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Help   string   `json:"help"`
	Labels []string `json:"labels"`
}

// LogLine is one log line, masked.
type LogLine struct {
	At     time.Time         `json:"at"`
	Fields map[string]string `json:"fields"`
	Line   string            `json:"line"`
}

// The personal data and secrets a log line may hold, and what replaces
// each (NFR-PRIV-02). A trace id or a visitor's hash is neither.
var masks = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`(?i)bearer\s+[^\s"',]+`), "Bearer <token>"},
	{regexp.MustCompile(`\beyJ[\w-]+\.[\w-]+\.[\w-]+`), "<token>"},
	{regexp.MustCompile(`\b(?:snt|sns|ss|sr)_[A-Za-z0-9_-]{16,}`), "<token>"},
	{regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api_key|apikey|otp|code)=[^\s&"',]+`), "$1=<masked>"},
	{regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+`), "<email>"},
	{regexp.MustCompile(`(?:\+84|\b84|\b0)[1-9]\d{8}\b`), "<phone>"},
}

// Mask hides emails, phone numbers and tokens in s and cuts it at LineCut.
func Mask(s string) string {
	for _, m := range masks {
		s = m.re.ReplaceAllString(s, m.with)
	}
	if r := []rune(s); len(r) > LineCut {
		s = string(r[:LineCut]) + "…"
	}
	return s
}
