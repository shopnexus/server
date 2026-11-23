package httpx

import (
	"context"
	"fmt"
	"iter"
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

// The tiers an operation's x-rate-limit in api/openapi.yaml may name; an
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
	// Exempt counts nothing: health checks, signed webhooks, forwardAuth.
	Exempt = Tier{Name: "exempt"}
)

var tiers = map[string]Tier{
	Read.Name: Read, Write.Name: Write, Auth.Name: Auth, Code.Name: Code,
	Costly.Name: Costly, Telemetry.Name: Telemetry, Exempt.Name: Exempt,
}

// Operations yields every operation of doc with its route.
func Operations(doc *openapi3.T) iter.Seq2[Route, *openapi3.Operation] {
	return func(yield func(Route, *openapi3.Operation) bool) {
		for path, item := range doc.Paths.Map() {
			for method, op := range item.Operations() {
				if !yield(Route{Method: strings.ToUpper(method), Path: path}, op) {
					return
				}
			}
		}
	}
}

// Limits holds every token bucket of this process; each process counts on
// its own.
type Limits struct {
	byRoute map[Route]Tier
	now     func() time.Time
	log     *slog.Logger
	refused metric.Int64Counter

	mu      sync.Mutex
	buckets map[bucketKey]*rate.Limiter
	swept   time.Time
}

type bucketKey struct {
	tier   string
	addr   netip.Addr
	caller uuid.UUID
}

// NewLimits reads each operation's x-rate-limit from doc. An unknown tier
// name is an error.
func NewLimits(doc *openapi3.T, now func() time.Time, log *slog.Logger) (*Limits, error) {
	l := &Limits{
		byRoute: map[Route]Tier{}, now: now, log: log, buckets: map[bucketKey]*rate.Limiter{},
		refused: telemetry.Must(otel.Meter(scope).Int64Counter(
			"http.server.rate_limited", metric.WithDescription("Requests refused 429 rate_limited, by tier, route and what was counted."))),
	}
	for route, op := range Operations(doc) {
		raw, ok := op.Extensions["x-rate-limit"]
		if !ok {
			continue
		}
		name, isString := raw.(string)
		t, ok := tiers[name]
		if !isString || !ok {
			return nil, fmt.Errorf("%s %s: x-rate-limit %v is not a tier", route.Method, route.Path, raw)
		}
		l.byRoute[route] = t
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
			if err := s.countAddress(r, s.tier); err != nil {
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
	return s.count(r.Context(), s.tier.Caller, bucketKey{tier: s.tier.Name, caller: caller})
}

// Limit counts the requests it wraps against t by client address, on top
// of the route's own tier: a module's limit on some requests of a route.
// t's name must differ from the shared tiers'. It lets everything through
// outside Limits.Middleware.
func Limit(t Tier) func(http.Handler) http.Handler {
	if _, shared := tiers[t.Name]; shared {
		panic("httpx: Limit with the shared tier name " + t.Name)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s, ok := r.Context().Value(limitKey{}).(*limitScope); ok {
				if err := s.countAddress(r, t); err != nil {
					WriteError(w, r, s.l.log, err)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *limitScope) countAddress(r *http.Request, t Tier) error {
	return s.count(r.Context(), t.Address, bucketKey{tier: t.Name, addr: addressKey(ClientIP(r))})
}

// count takes a token for key at limit.
func (s *limitScope) count(ctx context.Context, limit Rate, key bucketKey) error {
	if limit == (Rate{}) {
		return nil
	}
	wait, ok := s.l.take(key, limit)
	if ok {
		return nil
	}
	by := "address"
	if key.caller != (uuid.UUID{}) {
		by = "caller"
	}
	s.l.refused.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tier", key.tier), attribute.String("http.route", s.route), attribute.String("by", by)))
	return errRateLimited.WithRetryAfter(wait)
}

// take spends a token from key's bucket, or returns how long until the
// next one. Once a minute it drops the buckets that have refilled.
func (l *Limits) take(key bucketKey, r Rate) (time.Duration, bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) > time.Minute {
		for k, b := range l.buckets {
			if b.TokensAt(now) >= float64(b.Burst()) {
				delete(l.buckets, k)
			}
		}
		l.swept = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = rate.NewLimiter(rate.Limit(r.PerSecond), r.Burst)
		l.buckets[key] = b
	}
	if b.AllowN(now, 1) {
		return 0, true
	}
	return time.Duration((1 - b.TokensAt(now)) / r.PerSecond * float64(time.Second)), false
}

// addressKey is addr with an IPv6 address cut to its /64, which one
// subscriber usually holds whole.
func addressKey(addr netip.Addr) netip.Addr {
	if addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().Addr()
	}
	return addr
}
