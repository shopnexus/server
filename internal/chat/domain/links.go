package domain

import (
	"net/url"
	"regexp"
	"strings"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// LinkKind is what a ShopNexus link in a message points at.
type LinkKind string

const (
	LinkListing  LinkKind = "listing"
	LinkSeller   LinkKind = "seller"
	LinkOrder    LinkKind = "order"
	LinkCategory LinkKind = "category"
	LinkSearch   LinkKind = "search"
)

// MaxLinks is how many links of a message become cards; the rest stay text.
const MaxLinks = 5

// Link is a ShopNexus link found in a message's text. URL is the text as
// written, so a client can find it in the message. ID is set for a
// listing, seller or order, Code for a category, Query for a search. A
// listing's web address names it by slug instead (Slug), which module
// catalog resolves.
type Link struct {
	URL   string
	Kind  LinkKind
	ID    uuid.UUID
	Slug  string
	Code  string
	Query string
}

var (
	linkCandidate = regexp.MustCompile(`(?i)\b(?:https?://|shopnexus://)[^\s<>"']+`)
	categoryCode  = regexp.MustCompile(`^[a-z0-9_]+(?:\.[a-z0-9_]+)?$`)
)

// Links finds the ShopNexus links in text: the website's pages on one of
// hosts (listings, sellers, orders, categories, search) and the app's
// shopnexus:// links. The same link twice counts once; at most MaxLinks.
func Links(text string, hosts []string) []Link {
	var out []Link
	seen := map[string]bool{}
	for _, raw := range linkCandidate.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ".,;:!?)]}")
		l, ok := parseLink(raw, hosts)
		if !ok || seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, l)
		if len(out) == MaxLinks {
			break
		}
	}
	return out
}

func parseLink(raw string, hosts []string) (Link, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, false
	}
	var parts []string
	switch strings.ToLower(u.Scheme) {
	case "shopnexus":
		// In an app link the host names the kind, listing for listings.
		parts = append([]string{u.Host + "s"}, segments(u.Path)...)
	case "http", "https":
		if !ownHost(u.Hostname(), hosts) {
			return Link{}, false
		}
		parts = segments(u.Path)
	default:
		return Link{}, false
	}
	l := Link{URL: raw}
	switch {
	case len(parts) == 1 && parts[0] == "search":
		l.Kind, l.Query = LinkSearch, strings.TrimSpace(u.Query().Get("q"))
		return l, l.Query != ""
	case len(parts) != 2:
		return Link{}, false
	case parts[0] == "categories":
		l.Kind, l.Code = LinkCategory, parts[1]
		return l, categoryCode.MatchString(l.Code)
	case parts[0] == "listings" && catalogdomain.CodeOf(parts[1]) != "":
		l.Kind, l.Slug = LinkListing, parts[1]
		return l, true
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Link{}, false
	}
	l.ID = id
	switch parts[0] {
	case "listings":
		l.Kind = LinkListing
	case "sellers":
		l.Kind = LinkSeller
	case "orders":
		l.Kind = LinkOrder
	default:
		return Link{}, false
	}
	return l, true
}

func segments(path string) []string {
	var out []string
	for s := range strings.SplitSeq(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ownHost is one of hosts, or its www. form.
func ownHost(host string, hosts []string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	for _, h := range hosts {
		if strings.TrimPrefix(strings.ToLower(h), "www.") == host {
			return true
		}
	}
	return false
}
