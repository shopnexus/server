package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/getkin/kin-openapi/openapi3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/telemetry"
)

var errRateLimited = apperr.New(apperr.KindRateLimited, "rate_limited", "too many requests; slow down")

// Rate is a token bucket: PerSecond requests a second in bursts of up to
// Burst. The zero Rate counts nothing.
type Rate struct {
	PerSecond float64
	Burst     int
}

// Tier is one rate limit policy. Address counts each client address, an
// IPv6 /64 as one; Caller counts each signed-in account or console member.
type Tier struct {
	Name            string
	Address, Caller Rate
}

// The tiers. An operation's x-rate-limit in api/openapi.yaml names one; an
// operation without it is Read for GET and Write otherwise. Carrier NAT
// puts many phones on one address, so Address is the looser of the two.
var (
	Read   = Tier{"read", Rate{50, 1000}, Rate{20, 400}}
	Write  = Tier{"write", Rate{10, 200}, Rate{5, 100}}
	Auth   = Tier{"auth", Rate{1, 60}, Rate{0.2, 10}}       // sign-in with a proof
	Code   = Tier{"code", Rate{0.1, 10}, Rate{1.0 / 30, 5}} // mails a code
	Costly = Tier{"costly", Rate{1, 30}, Rate{0.5, 20}}     // a model or another site answers
	// Telemetry is a page's or the app's batch of client events.
	Telemetry = Tier{"telemetry", Rate{2, 60}, Rate{}}
	// GuestSearch is a guest's search with q; discovery applies it with Limit.
	GuestSearch = Tier{"guest-search", Rate{2, 30}, Rate{}}
	// Exempt counts nothing: health checks, signed webhooks, forwardAuth.
	Exempt = Tier{Name: "exempt"}
)

var tiers = map[string]Tier{}

func init() {
	for _, t := range []Tier{Read, Write, Auth, Code, Costly, Telemetry, GuestSearch, Exempt} {
		tiers[t.Name] = t
	}
}

// TierNamed returns the tier x-rate-limit calls name.
func TierNamed(name string) (Tier, bool) {
	t, ok := tiers[name]
	return t, ok
}

// Limits holds every token bucket of this process; each process counts on
// its own.
type Limits struct {
	byRoute map[Route]Tier
	now     func() time.Time
	log     *slog.Logger
	refused metric.Int64Counter

	mu      sync.Mutex
	buckets map[bucketKey]*bucket
	swept   time.Time
}

type bucketKey struct {
	tier   string
	addr   netip.Addr
	caller uuid.UUID
}

type bucket struct {
	tokens *rate.Limiter
	last   time.Time
	full   time.Duration // idle this long, the bucket is full and dropped
}

// NewLimits reads each operation's x-rate-limit from doc. An unknown tier
// name is an error.
func NewLimits(doc *openapi3.T, now func() time.Time, log *slog.Logger) (*Limits, error) {
	l := &Limits{
		byRoute: map[Route]Tier{}, now: now, log: log, buckets: map[bucketKey]*bucket{},
		refused: telemetry.Must(otel.Meter("github.com/shopnexus/server/internal/platform/httpx").Int64Counter(
			"http.server.rate_limited", metric.WithDescription("Requests refused 429 rate_limited, by tier, route and what was counted."))),
	}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			raw, ok := op.Extensions["x-rate-limit"]
			if !ok {
				continue
			}
			name, _ := raw.(string)
			t, ok := tiers[name]
			if !ok {
				return nil, fmt.Errorf("%s %s: x-rate-limit %v is not a tier", method, path, raw)
			}
			l.byRoute[Route{Method: strings.ToUpper(method), Path: path}] = t
		}
	}
	return l, nil
}

// TierOf is the tier of the route at method and path pattern; "" is a path
// no route serves.
func (l *Limits) TierOf(method, pattern string) Tier {
	if t, ok := l.byRoute[Route{Method: method, Path: pattern}]; ok {
		return t
	}
	if method == http.MethodGet || method == http.MethodHead {
		return Read
	}
	return Write
}

type limitKey struct{}

type limitScope struct {
	l     *Limits
	tier  Tier
	route string
}

// Middleware counts each request against its route's tier by client
// address and answers 429 rate_limited past it. route maps a request to
// its path pattern, as Router.Route does. LimitCaller counts the caller
// later, once the auth middleware knows who it is.
func (l *Limits) Middleware(route func(method, path string) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pattern := route(r.Method, r.URL.Path)
			s := &limitScope{l: l, tier: l.TierOf(r.Method, pattern), route: pattern}
			if err := s.count(r.Context(), s.tier, bucketKey{addr: addressKey(ClientIP(r))}, "address"); err != nil {
				WriteError(w, r, l.log, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), limitKey{}, s)))
		})
	}
}

// LimitCaller counts the request against its route's tier for caller, an
// account or console member ID, and returns rate_limited past it. It
// returns nil on a request that did not pass Limits.Middleware.
func LimitCaller(r *http.Request, caller uuid.UUID) error {
	s, ok := r.Context().Value(limitKey{}).(*limitScope)
	if !ok {
		return nil
	}
	return s.count(r.Context(), Tier{Name: s.tier.Name, Address: s.tier.Caller}, bucketKey{caller: caller}, "caller")
}

// Limit counts the requests it wraps against t by client address, on top
// of the route's own tier: for a limit that applies to some requests of a
// route only. It lets everything through outside Limits.Middleware.
func Limit(t Tier, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s, ok := r.Context().Value(limitKey{}).(*limitScope); ok {
				if err := s.count(r.Context(), t, bucketKey{addr: addressKey(ClientIP(r))}, "address"); err != nil {
					WriteError(w, r, log, err)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// count takes a token for key from t.Address.
func (s *limitScope) count(ctx context.Context, t Tier, key bucketKey, by string) error {
	if t.Address == (Rate{}) {
		return nil
	}
	key.tier = t.Name
	wait, ok := s.l.take(key, t.Address)
	if ok {
		return nil
	}
	s.l.refused.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tier", t.Name), attribute.String("http.route", s.route), attribute.String("by", by)))
	return errRateLimited.WithRetryAfter(wait)
}

// take spends a token from key's bucket, or returns how long until the
// next one.
func (l *Limits) take(key bucketKey, r Rate) (time.Duration, bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) > time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > b.full {
				delete(l.buckets, k)
			}
		}
		l.swept = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{
			tokens: rate.NewLimiter(rate.Limit(r.PerSecond), r.Burst),
			full:   time.Duration(float64(r.Burst) / r.PerSecond * float64(time.Second)),
		}
		l.buckets[key] = b
	}
	b.last = now
	if b.tokens.AllowN(now, 1) {
		return 0, true
	}
	return time.Duration((1 - b.tokens.TokensAt(now)) / r.PerSecond * float64(time.Second)), false
}

// addressKey is addr with an IPv6 address cut to its /64, which one
// subscriber usually holds whole.
func addressKey(addr netip.Addr) netip.Addr {
	if addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().Addr()
	}
	return addr
}
