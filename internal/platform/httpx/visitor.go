package httpx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/netip"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/geoip"
)

// visit is who made a request, for the access log line and the request
// metric of Trace. Visitors fills it.
type visit struct {
	client  string // android, website, console or other
	country string // ISO 3166 code, or ""
	visitor string // a:<account>, m:<member>, g:<hash of the address and day>, or ""
}

type visitKey struct{}

// Visitors notes the client app, the country and a visitor id of each
// request for Trace, which must be outside it, as must ResolveClientIP.
// A request from a trusted proxy itself (the website rendering with no
// buyer behind it) and a health probe get no visitor.
func Visitors(geo *geoip.DB, trusted []netip.Prefix, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v, ok := r.Context().Value(visitKey{}).(*visit)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			ctx, seen := authn.Track(r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))

			v.client = clientOf(r)
			ip := ClientIP(r)
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || !ip.IsValid() || isTrusted(ip, trusted) {
				return
			}
			v.country = geo.Country(ip)
			switch {
			case seen.Account != uuid.Nil():
				v.visitor = "a:" + seen.Account.String()
			case seen.Member != uuid.Nil():
				v.visitor = "m:" + seen.Member.String()
			default:
				// A guest is their address for the day; the hash keeps the
				// address itself out of the logs.
				sum := sha256.Sum256([]byte(now().UTC().Format(time.DateOnly) + " " + ip.String()))
				v.visitor = "g:" + hex.EncodeToString(sum[:6])
			}
		})
	}
}

// clientOf names the app a request came from. The website's server calls
// with Node's fetch, the Android app with OkHttp.
func clientOf(r *http.Request) string {
	ua := r.UserAgent()
	switch {
	case strings.HasPrefix(r.URL.Path, "/v1/console/"):
		return "console"
	case strings.HasPrefix(ua, "okhttp/"), strings.HasPrefix(ua, "Dalvik/"), strings.HasPrefix(ua, "Ktor"):
		return "android"
	case ua == "node", strings.HasPrefix(ua, "undici"), strings.HasPrefix(ua, "Mozilla/"):
		return "website"
	default:
		return "other"
	}
}

// withVisit hands Trace a visit for Visitors to fill.
func withVisit(ctx context.Context) (context.Context, *visit) {
	v := &visit{}
	return context.WithValue(ctx, visitKey{}, v), v
}
