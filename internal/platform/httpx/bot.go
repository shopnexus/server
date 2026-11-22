package httpx

import (
	"context"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
)

// ForwardedUserAgent is where a trusted proxy (the website) puts its
// browser's User-Agent; its own calls carry Node's.
const ForwardedUserAgent = "X-Forwarded-User-Agent"

// botUA matches crawlers, link previews, headless browsers, scanners and
// HTTP libraries. Bots that pose as a browser get past it.
var botUA = regexp.MustCompile(`(?i)bot\b|bot[/_;-]|crawl|spider|slurp|scrap|archiv|preview|externalhit|headless|lighthouse|pagespeed|phantomjs|selenium|puppeteer|playwright|python|curl|wget|httpie|go-http|java/|apache-http|libwww|perl/|ruby|axios|scan|monitor|uptime|check|validator|feed|^whatsapp|\+https?://`)

// A phone brand that botUA's bot\b would catch.
var botFalse = strings.NewReplacer("cubot", "", "CUBOT", "")

// userAgentOf is the User-Agent of whoever is behind r: the browser a
// trusted proxy forwarded, else the caller's own. forwarded says which.
func userAgentOf(r *http.Request, trusted []netip.Prefix) (ua string, forwarded bool) {
	if vals := r.Header.Values(ForwardedUserAgent); len(vals) > 0 {
		if peer, err := netip.ParseAddrPort(r.RemoteAddr); err == nil && isTrusted(peer.Addr().Unmap(), trusted) {
			return vals[0], true
		}
	}
	return r.UserAgent(), false
}

// isBot reports whether ua is a program, not a person. A browser a proxy
// forwarded must say Mozilla/, as every browser does, and so must a
// caller that is none of ShopNexus's apps.
func isBot(ua string, forwarded bool, client string) bool {
	if botUA.MatchString(botFalse.Replace(ua)) {
		return true
	}
	return !strings.HasPrefix(ua, "Mozilla/") && (forwarded || client == "other")
}

// Bot reports whether the request behind ctx came from a bot, as Visitors
// judged it. Analytics leave such requests out.
func Bot(ctx context.Context) bool {
	v, ok := ctx.Value(visitKey{}).(*visit)
	return ok && v.bot
}
