package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	t.Parallel()
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cases := []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{"direct, header ignored", "203.0.113.7:5000", []string{"198.51.100.1"}, nil, "203.0.113.7"},
		{"untrusted remote, header ignored", "203.0.113.7:5000", []string{"198.51.100.1"}, proxies, "203.0.113.7"},
		{"trusted proxy", "10.0.0.1:5000", []string{"198.51.100.1"}, proxies, "198.51.100.1"},
		{"spoofed left entry ignored", "10.0.0.1:5000", []string{"6.6.6.6, 198.51.100.1, 10.0.0.2"}, proxies, "198.51.100.1"},
		{"two headers joined", "10.0.0.1:5000", []string{"6.6.6.6", "198.51.100.1"}, proxies, "198.51.100.1"},
		{"no header", "10.0.0.1:5000", nil, proxies, "10.0.0.1"},
		{"garbage hop", "10.0.0.1:5000", []string{"nonsense"}, proxies, "10.0.0.1"},
		{"ipv6", "[2001:db8::1]:443", nil, nil, "2001:db8::1"},
		{"ipv4-mapped ipv6", "[::ffff:203.0.113.7]:80", nil, nil, "203.0.113.7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
			r.RemoteAddr = c.remote
			for _, v := range c.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := ClientIP(r, c.trusted); got.String() != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}
