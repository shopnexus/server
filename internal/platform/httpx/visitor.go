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
	client  string // android, website, console, bot or other
	country string // ISO 3166 code, or ""
	visitor string // a:<account>, m:<member>, g: or b:<hash of the address and day>, or ""
	bot     bool   // a program, not a person (isBot)
	ua      string // a bot's User-Agent, cut short
}

// maxBotUA is how much of a bot's User-Agent the access log keeps.
const maxBotUA = 160

type visitKey struct{}

// Visitors notes the client app, the country and a visitor id of each
// request for Trace, which must be outside it, as must ResolveClientIP.
// A request from a trusted proxy itself (the website rendering with no
// buyer behind it) and a health probe get no visitor. A bot is client bot
// and visitor b:, whoever it signed in as.
func Visitors(geo *geoip.DB, trusted []netip.Prefix, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v, ok := r.Context().Value(visitKey{}).(*visit)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			v.client = clientOf(r)
			ip := ClientIP(r)
			counted := r.URL.Path != "/healthz" && r.URL.Path != "/readyz" && ip.IsValid() && !isTrusted(ip, trusted)
			// Known before the handler runs, so analytics can skip a bot.
			if ua, forwarded := userAgentOf(r, trusted); counted && isBot(ua, forwarded, v.client) {
				v.client, v.bot, v.ua = "bot", true, cut(ua, maxBotUA)
			}
			ctx, seen := authn.Track(r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))

			if !counted {
				return
			}
			v.country = geo.Country(ip)
			switch {
			case v.bot:
				v.visitor = "b:" + daily(now, ip)
			case seen.Account != uuid.Nil():
				v.visitor = "a:" + seen.Account.String()
			case seen.Member != uuid.Nil():
				v.visitor = "m:" + seen.Member.String()
			default:
				v.visitor = "g:" + daily(now, ip)
			}
		})
	}
}

// daily is a guest's id: their address for the day, hashed to keep the
// address itself out of the logs.
func daily(now func() time.Time, ip netip.Addr) string {
	sum := sha256.Sum256([]byte(now().UTC().Format(time.DateOnly) + " " + ip.String()))
	return hex.EncodeToString(sum[:6])
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
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
