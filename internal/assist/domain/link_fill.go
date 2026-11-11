package domain

import (
	"encoding/json"
	"regexp"
	"strings"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
)

// MaxCopied is how many of a listing's photos a link read copies: as many
// as a listing takes.
const MaxCopied = catalogdomain.MaxPhotos

// FailListingNotFound is a link read of a listing Chợ Tốt does not have on
// sale, or one with no photo that could be copied.
const FailListingNotFound Failure = "listing_not_found"

// LinkAnswer is what the model returns for a listing, in LinkAnswerSchema's
// shape. The price is not in it: the listing states it.
type LinkAnswer struct {
	IsItem         bool                 `json:"is_item"`
	Title          string               `json:"title"`
	CategoryCode   string               `json:"category_code"`
	Condition      string               `json:"condition"`
	Specs          []catalogdomain.Spec `json:"specs"`
	Description    string               `json:"description"`
	Negotiable     bool                 `json:"negotiable"`
	ContactRemoved bool                 `json:"contact_removed"`
	Estimate       *Range               `json:"estimate"`
}

// LinkAnswerSchemaName names LinkAnswerSchema in the request.
const LinkAnswerSchemaName = "link_read"

// LinkAnswerSchema is LinkAnswer as a strict JSON schema.
var LinkAnswerSchema = json.RawMessage(`{"type":"object","additionalProperties":false,
"required":["is_item","title","category_code","condition","specs","description","negotiable","contact_removed","estimate"],
"properties":{"is_item":{"type":"boolean"},"title":{"type":"string"},"category_code":{"type":"string"},
"condition":{"type":"string","enum":["new","like_new","used"]},
"specs":{"type":"array","maxItems":20,"items":{"type":"object","additionalProperties":false,"required":["label","value"],
"properties":{"label":{"type":"string"},"value":{"type":"string"}}}},
"description":{"type":"string"},"negotiable":{"type":"boolean"},"contact_removed":{"type":"boolean"},
"estimate":{"anyOf":[{"type":"null"},{"type":"object","additionalProperties":false,"required":["low","median","high"],
"properties":{"low":{"type":"integer"},"median":{"type":"integer"},"high":{"type":"integer"}}}]}}}`)

// CleanLink turns the model's answer and the listing's price into a draft
// without photos, the model's price range, and whether a contact was taken
// out of the description, by the model or by StripPhones after it.
func CleanLink(a LinkAnswer, price int64, cats []catalogdomain.Category) (Draft, *Range, bool, error) {
	d, err := cleanDraft(a.IsItem, a.Title, a.CategoryCode, a.Condition, a.Specs, a.Description, cats)
	if err != nil {
		return Draft{}, nil, false, err
	}
	description, stripped := StripPhones(d.Description)
	d.Description, d.AllowOffers = description, a.Negotiable
	if price >= catalogdomain.MinPrice && price <= catalogdomain.MaxPrice {
		d.Price = &price
	}
	return d, cleanRange(a.Estimate), a.ContactRemoved || stripped, nil
}

// phone is a Vietnamese phone number: 0 or +84, then 9 digits, with single
// spaces, dots or dashes between them, standing alone. A price such as
// 1.200.000.000 has 8 digits after its first 0 and stays.
var phone = regexp.MustCompile(`(?:\+84|\b0)(?:[ .\-]?\d){9}\b`)

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
