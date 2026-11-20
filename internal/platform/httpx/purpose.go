package httpx

import (
	"net/http"
	"strings"
)

// Prefetch reports whether r is a browser's speculative fetch: an item of
// its Sec-Purpose header, a structured-field list, is the token prefetch,
// whatever its parameters (prefetch;prerender). A use case that writes on a
// read leaves the write out for one.
func Prefetch(r *http.Request) bool {
	for item := range strings.SplitSeq(r.Header.Get("Sec-Purpose"), ",") {
		token, _, _ := strings.Cut(item, ";")
		if strings.TrimSpace(token) == "prefetch" {
			return true
		}
	}
	return false
}
