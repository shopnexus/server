package domain

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/textkey"
)

const (
	// ImportStale is how long an import may run; one older is taken as
	// stopped and may start again.
	ImportStale = time.Hour
	// DraftLife is how long an import that waits as a draft keeps its
	// photos, as a listing draft does.
	DraftLife = 30 * 24 * time.Hour
)

// ImportState is where an import of one Chợ Tốt listing is.
type ImportState string

const (
	Importing   ImportState = "importing"
	Listed      ImportState = "listed"
	Drafted     ImportState = "draft"
	Skipped     ImportState = "skipped"
	Unsupported ImportState = "unsupported"
)

// Why a listing was not brought over. A refusal of the listing's post
// keeps its own code.
const (
	ReasonRealEstate  = "real_estate"
	ReasonNoItem      = "no_item"
	ReasonNoPhotos    = "no_photos"
	ReasonAdGone      = "ad_gone"
	ReasonNotYours    = "not_yours"
	ReasonUnavailable = "unavailable"
)

// FieldCondition is the field an import sets to used when the listing does
// not state it.
const FieldCondition = "condition"

// Draft is a listing an import could not post, as far as it was filled.
type Draft struct {
	Title        string                  `json:"title"`
	Price        *int64                  `json:"price,omitempty"`
	Description  string                  `json:"description"`
	CategoryCode string                  `json:"category_code"`
	Condition    catalogdomain.Condition `json:"condition"`
	Specs        []catalogdomain.Spec    `json:"specs"`
	Photos       []uuid.UUID             `json:"photos"`
	CommuneCode  string                  `json:"commune_code"`
}

// Listing is the draft as the listing to post.
func (d Draft) Listing() catalogdomain.ListingInput {
	var price int64
	if d.Price != nil {
		price = *d.Price
	}
	return catalogdomain.ListingInput{
		Title: d.Title, Price: price, Description: d.Description, CategoryCode: d.CategoryCode, Condition: d.Condition,
		AllowOffers: true, CommuneCode: d.CommuneCode, Photos: d.Photos, Specs: d.Specs,
	}
}

// Import is an account's import of one Chợ Tốt listing. OID is the profile
// it came from, AdPrice the listing's price there as last seen, AdGone
// whether its copy was hidden because the listing left Chợ Tốt. Review
// names the fields to check: on a listing, those set without the listing
// stating them; on a draft, those the rules refused.
type Import struct {
	ID              uuid.UUID   `json:"id"`
	AccountID       uuid.UUID   `json:"account_id"`
	ListID          int64       `json:"list_id"`
	OID             string      `json:"oid"`
	State           ImportState `json:"state"`
	ListingID       *uuid.UUID  `json:"listing_id,omitempty"`
	Review          []string    `json:"review"`
	MissingSpecKeys []string    `json:"missing_spec_keys"`
	Reason          string      `json:"reason,omitempty"`
	Draft           *Draft      `json:"draft,omitempty"`
	AdPrice         int64       `json:"ad_price"`
	AdGone          bool        `json:"ad_gone"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// NewImport starts an import.
func NewImport(id, account uuid.UUID, listID int64, oid string, now time.Time) Import {
	return Import{
		ID: id, AccountID: account, ListID: listID, OID: oid, State: Importing, Review: []string{}, MissingSpecKeys: []string{},
		UpdatedAt: now,
	}
}

// Settled reports an import a new request answers as it is: a listing, a
// draft before DraftLife, an unsupported listing or an import running for
// less than ImportStale. A skipped import, a stale one or an expired draft
// may start again.
func (i *Import) Settled(now time.Time) bool {
	switch i.State {
	case Listed, Unsupported:
		return true
	case Drafted:
		return now.Sub(i.UpdatedAt) < DraftLife
	case Importing:
		return now.Sub(i.UpdatedAt) < ImportStale
	case Skipped:
		return false
	default:
		return false
	}
}

// Listed records the listing the import posted and the fields to review.
func (i *Import) Listed(listing uuid.UUID, review []string, adPrice int64, now time.Time) {
	i.State, i.ListingID, i.Review, i.MissingSpecKeys = Listed, &listing, append([]string{}, review...), []string{}
	i.Reason, i.Draft, i.AdPrice, i.UpdatedAt = "", nil, adPrice, now
}

// Drafted keeps d as a draft, the fields the rules refused under review.
func (i *Import) Drafted(d Draft, refused *apperr.Error, adPrice int64, now time.Time) {
	review := make([]string, 0, len(refused.Fields))
	for f := range refused.Fields {
		review = append(review, f)
	}
	slices.Sort(review)
	i.State, i.ListingID, i.Review, i.MissingSpecKeys = Drafted, nil, review, slices.Clone(refused.Details.MissingSpecKeys)
	if i.MissingSpecKeys == nil {
		i.MissingSpecKeys = []string{}
	}
	i.Reason, i.Draft, i.AdPrice, i.UpdatedAt = "", &d, adPrice, now
}

// Ended ends the import without a listing: skipped, which may start
// again, or unsupported, which never does.
func (i *Import) Ended(state ImportState, reason string, now time.Time) {
	i.State, i.ListingID, i.Review, i.MissingSpecKeys = state, nil, []string{}, []string{}
	i.Reason, i.Draft, i.UpdatedAt = reason, nil, now
}

// AdState is what one of the profile's listings on Chợ Tốt is here.
type AdState string

const (
	AdImportable  AdState = "importable"
	AdImporting   AdState = "importing"
	AdImported    AdState = "imported"
	AdAlreadyHere AdState = "already_here"
	AdUnsupported AdState = "unsupported"
)

// AdStatus is an AdState with its reason and listing.
type AdStatus struct {
	State     AdState
	Reason    string
	ListingID *uuid.UUID
}

// StatusOf is what a listing on Chợ Tốt is here, given whether it is real
// estate, the account's import of it, nil for none, and the account's
// listing that looks like it, nil for none.
func StatusOf(realEstate bool, imp *Import, here *uuid.UUID, now time.Time) AdStatus {
	if imp != nil && imp.Settled(now) {
		switch imp.State {
		case Listed:
			return AdStatus{State: AdImported, ListingID: imp.ListingID}
		case Drafted:
			return AdStatus{State: AdImported}
		case Unsupported:
			return AdStatus{State: AdUnsupported, Reason: imp.Reason}
		case Importing, Skipped:
			return AdStatus{State: AdImporting}
		default:
		}
	}
	switch {
	case realEstate:
		return AdStatus{State: AdUnsupported, Reason: ReasonRealEstate}
	case here != nil:
		return AdStatus{State: AdAlreadyHere, ListingID: here}
	case imp != nil && imp.State == Skipped:
		return AdStatus{State: AdImportable, Reason: imp.Reason}
	default:
		return AdStatus{State: AdImportable}
	}
}

// Mine is one of the account's listings on sale, as SameItem compares it.
type Mine struct {
	ID    uuid.UUID
	Title string
	Price int64
}

// SameItem finds the account's listing that a Chợ Tốt listing of title and
// price looks like: the same price and a title whose words hold the other's,
// marks, case and punctuation aside, at least two words.
func SameItem(title string, price int64, mine []Mine) *uuid.UUID {
	words := titleWords(title)
	for _, m := range mine {
		if m.Price != price {
			continue
		}
		other := titleWords(m.Title)
		short, long := words, other
		if len(short) > len(long) {
			short, long = long, short
		}
		if len(short) >= 2 && strings.Contains(" "+strings.Join(long, " ")+" ", " "+strings.Join(short, " ")+" ") {
			id := m.ID
			return &id
		}
	}
	return nil
}

// titleWords are a title's words, folded, without punctuation.
func titleWords(s string) []string {
	return strings.FieldsFunc(textkey.Fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
