package httpx

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP returns the caller's address. A request from a trusted proxy is
// traced back through X-Forwarded-For from the right, and the first address
// that is not a trusted proxy is the client. Entries left of it are whatever
// the client sent and are ignored.
func ClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	remote, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	addr := remote.Addr().Unmap()
	if !isTrusted(addr, trusted) {
		return addr
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
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
