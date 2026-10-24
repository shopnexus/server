package httpx

import (
	"context"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

type clientIPKey struct{}

// ResolveClientIP works out each request's client address once, for
// ClientIP. A request from a trusted proxy is traced back through
// X-Forwarded-For from the right, and the first address that is not a
// trusted proxy is the client. Entries left of it are whatever the client
// sent and are ignored.
func ResolveClientIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), clientIPKey{}, clientIPOf(r, trusted))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClientIP is the address ResolveClientIP found, or the zero Addr on a
// request that did not pass through it.
func ClientIP(r *http.Request) netip.Addr {
	ip, ok := r.Context().Value(clientIPKey{}).(netip.Addr)
	if !ok {
		return netip.Addr{}
	}
	return ip
}

func clientIPOf(r *http.Request, trusted []netip.Prefix) netip.Addr {
	remote, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	addr := remote.Addr().Unmap()
	if !isTrusted(addr, trusted) {
		return addr
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for _, raw := range slices.Backward(hops) {
		hop, err := netip.ParseAddr(strings.TrimSpace(raw))
		if err != nil {
			return addr
		}
		hop = hop.Unmap()
		if !isTrusted(hop, trusted) {
			return hop
		}
		addr = hop
	}
	return addr
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
