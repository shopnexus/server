package domain

import (
	"regexp"
	"strings"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// MaxCopied is how many of a listing's photos a link read copies: as many
// as a listing takes.
const MaxCopied = catalogdomain.MaxPhotos

// LinkText is a link read's text as the model writes it, with its phone numbers taken
// out, and a number it may still be writing at its end too.
func LinkText(raw string) Text {
	t := PartialText(raw, catalogdomain.MaxDescription)
	t.Description, _ = StripPhones(t.Description)
	t.Description = strings.TrimRight(partialPhone.ReplaceAllString(t.Description, ""), " ")
	return t
}

// LinkDraft is the draft of a link read's reply at the listing's price,
// and whether a contact was taken out of its description.
func LinkDraft(r Reply, price int64) (Draft, bool, error) {
	d, err := textDraft(r, catalogdomain.MaxDescription)
	if err != nil {
		return Draft{}, false, err
	}
	var stripped bool
	d.Description, stripped = StripPhones(d.Description)
	if price >= catalogdomain.MinPrice && price <= catalogdomain.MaxPrice {
		d.Price = &price
	}
	return d, stripped, nil
}

// phone is a Vietnamese phone number: 0 or +84, then 9 digits, with single
// spaces, dots or dashes between them, standing alone. A price such as
// 1.200.000.000 has 8 digits after its first 0 and stays.
var phone = regexp.MustCompile(`(?:\+84|\b0)(?:[ .\-]?\d){9}\b`)

// partialPhone is the start of a phone number at the end of a text.
var partialPhone = regexp.MustCompile(`(?:\+|\b0)[\d .\-]*$`)

var (
	spaces      = regexp.MustCompile(` {2,}`)
	beforePunct = regexp.MustCompile(` +([,.;:!?])`)
)

// StripPhones takes the phone numbers out of s, tidies the spaces they
// leave line by line, and reports whether it took any out.
func StripPhones(s string) (string, bool) {
	if !phone.MatchString(s) {
		return s, false
	}
	lines := strings.Split(phone.ReplaceAllString(s, ""), "\n")
	for i, l := range lines {
		l = spaces.ReplaceAllString(l, " ")
		lines[i] = strings.TrimRight(beforePunct.ReplaceAllString(l, "$1"), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), true
}
