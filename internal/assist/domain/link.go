package domain

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	// SiteChotot is the one site a link read reads.
	SiteChotot = "chotot.com"
	// MaxLinkLength is the longest link a read takes.
	MaxLinkLength = 2048
)

// Link is a listing on another site, as the seller pasted it: the site, the
// listing's ID there, and the link without what tracks the share.
type Link struct {
	Site   string `json:"site"`
	ListID int64  `json:"list_id"`
	URL    string `json:"url"`
}

// listIDSegment is a Chợ Tốt listing's last path segment: its ID, with or
// without ".htm".
var listIDSegment = regexp.MustCompile(`^(\d{1,19})(?:\.htm)?$`)

// ParseLink reads a pasted link. Text that is not an http or https URL is
// ErrNotALink; a URL off chotot.com and its subdomains, or without a
// listing ID, is ErrUnsupportedSource. The query and fragment are dropped,
// and the link is rewritten to the short form Chợ Tốt redirects from.
func ParseLink(s string) (Link, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > MaxLinkLength {
		return Link{}, ErrNotALink
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return Link{}, ErrNotALink
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host != SiteChotot && !strings.HasSuffix(host, "."+SiteChotot) {
		return Link{}, ErrUnsupportedSource
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	m := listIDSegment.FindStringSubmatch(segments[len(segments)-1])
	if m == nil {
		return Link{}, ErrUnsupportedSource
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || id <= 0 {
		return Link{}, ErrUnsupportedSource
	}
	return Link{Site: SiteChotot, ListID: id, URL: "https://www.chotot.com/" + strconv.FormatInt(id, 10) + ".htm"}, nil
}
