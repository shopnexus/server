package domain

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// MaxProfileURL is the longest address a link takes.
const MaxProfileURL = 2048

var oidPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// ParseProfileURL reads a profile's ID from its address,
// https://www.chotot.com/user/<32 hex digits>, with or without www, the
// scheme, a trailing slash or a query. Anything else is
// ErrInvalidProfileURL.
func ParseProfileURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > MaxProfileURL {
		return "", ErrInvalidProfileURL
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", ErrInvalidProfileURL
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host != "chotot.com" && host != "www.chotot.com" {
		return "", ErrInvalidProfileURL
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) != 2 || segments[0] != "user" || !oidPattern.MatchString(segments[1]) {
		return "", ErrInvalidProfileURL
	}
	return strings.ToLower(segments[1]), nil
}

// ProfileURL is the address of profile oid.
func ProfileURL(oid string) string { return "https://www.chotot.com/user/" + oid }

// AdURL is the address of listing listID on Chợ Tốt.
func AdURL(listID int64) string {
	return "https://www.chotot.com/" + strconv.FormatInt(listID, 10) + ".htm"
}
