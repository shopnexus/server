package domain

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// maxSlugWords caps the title part of a slug, cut at a word.
const maxSlugWords = 60

var listingCode = regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{8}$`)

// Slug is a listing's web address name: the title without accents, in
// lowercase words joined by hyphens, then the code. Only the code finds
// the listing, so an edited title keeps old addresses working.
func Slug(title, code string) string {
	if code == "" {
		return ""
	}
	var b strings.Builder
	gap := false
	for _, r := range norm.NFD.String(strings.ToLower(title)) {
		switch {
		case r == 'đ':
			r = 'd'
		case unicode.Is(unicode.Mn, r):
			continue
		default:
		}
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
			gap = b.Len() > 0
			continue
		}
		if gap {
			b.WriteByte('-')
			gap = false
		}
		b.WriteRune(r)
	}
	words := b.String()
	if len(words) > maxSlugWords {
		words = words[:maxSlugWords]
		if i := strings.LastIndexByte(words, '-'); i > 0 {
			words = words[:i]
		}
	}
	if words == "" {
		return code
	}
	return words + "-" + code
}

// CodeOf is the listing code a slug ends with, or "" when it ends with
// none.
func CodeOf(slug string) string {
	code := strings.ToLower(slug[strings.LastIndexByte(slug, '-')+1:])
	if !listingCode.MatchString(code) {
		return ""
	}
	return code
}
