package httpx

import (
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/shopnexus/server/internal/platform/apperr"
)

var errRateLimited = apperr.New(apperr.KindRateLimited, "rate_limited", "too many requests; slow down")

// RateLimit lets each client address make perSecond requests a second, in
// bursts of up to burst, and answers the rest 429 rate_limited. It reads
// the address ResolveClientIP found. Each process counts on its own.
func RateLimit(perSecond float64, burst int, now func() time.Time, log *slog.Logger) func(http.Handler) http.Handler {
	l := &limits{every: rate.Limit(perSecond), burst: burst, seen: map[netip.Addr]*limit{}}
	retry := time.Duration(float64(time.Second) / perSecond)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.allow(ClientIP(r), now()) {
				WriteError(w, r, log, errRateLimited.WithRetryAfter(retry))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// idleAfter is how long an address goes unseen before its bucket, full by
// then, is dropped.
const idleAfter = 10 * time.Minute

type limits struct {
	mu    sync.Mutex
	every rate.Limit
	burst int
	seen  map[netip.Addr]*limit
	swept time.Time
}

type limit struct {
	bucket *rate.Limiter
	last   time.Time
}

func (l *limits) allow(addr netip.Addr, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) > idleAfter {
		for a, s := range l.seen {
			if now.Sub(s.last) > idleAfter {
				delete(l.seen, a)
			}
		}
		l.swept = now
	}
	s, ok := l.seen[addr]
	if !ok {
		s = &limit{bucket: rate.NewLimiter(l.every, l.burst)}
		l.seen[addr] = s
	}
	s.last = now
	return s.bucket.AllowN(now, 1)
}
