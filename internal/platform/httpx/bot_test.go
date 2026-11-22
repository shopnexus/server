package httpx

import (
	"net/http"
	"net/netip"
	"testing"
)

func TestIsBot(t *testing.T) {
	t.Parallel()
	chrome := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"
	cases := []struct {
		ua        string
		forwarded bool
		client    string
		bot       bool
	}{
		{chrome, true, "website", false},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1 Zalo iOS/600", true, "website", false},
		{"Mozilla/5.0 (Linux; Android 13; CUBOT KINGKONG 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36", true, "website", false},
		{"Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", true, "website", true},
		{"Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)", true, "website", true},
		{"facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)", true, "website", true},
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/142.0.0.0 Safari/537.36", true, "website", true},
		{"Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.8103.78 Mobile Safari/537.36 (compatible; GoogleOther)", true, "website", true},
		{"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot", true, "website", true},
		{"meta-externalagent/1.1 (+https://developers.facebook.com/docs/sharing/webmasters/crawler)", true, "website", true},
		{"", true, "website", true},
		{"python-requests/2.32.3", false, "other", true},
		{"", false, "other", true},
		{"okhttp/5.5.1", false, "android", false},
		{"node", false, "website", false},
		{"Mozilla/5.0", false, "console", false},
	}
	for _, c := range cases {
		if got := isBot(c.ua, c.forwarded, c.client); got != c.bot {
			t.Errorf("isBot(%q, %v, %q) = %v", c.ua, c.forwarded, c.client, got)
		}
	}
}

// The website forwards its browser's User-Agent; a crawler behind it is a
// bot, and the handler already knows.
func TestVisitorsBot(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{netip.MustParsePrefix("10.42.0.0/16")}
	req := newReq(t, "/v1/search", "node", "10.42.0.7:5000")
	req.Header.Set("X-Forwarded-For", "66.249.66.1")
	req.Header.Set(ForwardedUserAgent, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	var seen bool
	m := visitLogWith(t, req, trusted, func(r *http.Request) { seen = Bot(r.Context()) })
	if !seen {
		t.Fatal("handler did not see a bot")
	}
	if v, ok := m["visitor"].(string); !ok || m["client"] != "bot" || len(v) != 14 || v[:2] != "b:" || m["ua"] == nil {
		t.Fatalf("got %v", m)
	}

	// Only a trusted proxy's header counts.
	req = newReq(t, "/v1/search", "Mozilla/5.0 (X11; Linux x86_64) Firefox/145.0", "113.161.0.1:5000")
	req.Header.Set(ForwardedUserAgent, "Googlebot/2.1")
	m = visitLogWith(t, req, trusted, func(r *http.Request) { seen = Bot(r.Context()) })
	if seen || m["client"] != "website" {
		t.Fatalf("spoofed header: bot %v, %v", seen, m)
	}
}

func visitLogWith(t *testing.T, req *http.Request, trusted []netip.Prefix, h func(r *http.Request)) map[string]any {
	t.Helper()
	return visitLogHandler(t, req, trusted, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h(r)
		w.WriteHeader(http.StatusNoContent)
	}))
}
