// Package domain holds module catalog's entities: the listing, one item a
// seller offers at one price, with its photos and specs, and the categories
// and regions it is filed under.
package domain

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/locale"
)

const (
	// MaxOnSale is how many listings a seller may have in an OnSale status.
	MaxOnSale      = 20
	MinPrice       = 10_000
	MaxPrice       = 1_000_000_000
	MaxTitle       = 70
	MaxDescription = 1500
	MaxPhotos      = 10
	MaxSpecs       = 20
	MaxSpecText    = 50
)

type Status string

const (
	StatusProcessing Status = "processing"
	StatusActive     Status = "active"
	StatusHidden     Status = "hidden"
	StatusRejected   Status = "rejected"
	// StatusReserved is a listing held while an order's parcel travels.
	StatusReserved Status = "reserved"
	StatusSold     Status = "sold"
	StatusDeleted  Status = "deleted"
)

// OnSale are the statuses that count toward a seller's cap and show in the
// on-sale pane.
var OnSale = []Status{StatusProcessing, StatusActive, StatusHidden, StatusRejected, StatusReserved}

// Public are the statuses anyone sees, on the seller's public page too.
var Public = []Status{StatusActive, StatusReserved}

// CountIn sums the counts of the given statuses.
func CountIn(counts map[Status]int, statuses []Status) int {
	n := 0
	for _, st := range statuses {
		n += counts[st]
	}
	return n
}

type Condition string

const (
	ConditionNew     Condition = "new"
	ConditionLikeNew Condition = "like_new"
	ConditionUsed    Condition = "used"
)

// PhotoKeys are the object keys of a processed photo and its thumbnail.
type PhotoKeys struct {
	Photo string `json:"photo"`
	Thumb string `json:"thumb"`
	// Copied is true for a photo the server copied from the seller's
	// listing on another site; processing says so.
	Copied bool `json:"copied,omitempty"`
}

// Photo is an upload of the listing; Keys is empty until it is processed.
type Photo struct {
	UploadID uuid.UUID `json:"upload_id"`
	Keys     PhotoKeys `json:"keys"`
}

// Spec is one of a listing's specs. Key names a spec the category asks for,
// whose Vietnamese name is then the label; a spec the seller names has no
// key and keeps their label.
type Spec struct {
	Key   string `json:"key,omitempty"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// ListingInput is a listing as the seller sends it, to post or to edit.
type ListingInput struct {
	Title        string
	Price        int64
	Description  string
	CategoryCode string
	Condition    Condition
	AllowOffers  bool
	CommuneCode  string
	Photos       []uuid.UUID
	Specs        []Spec
	// Spot is the seller's GPS point, already blurred; nil keeps the stored
	// one.
	Spot *Point
}

// Refs is what the service found in the category and region tables for a
// listing input.
type Refs struct {
	CategoryIsLeaf bool
	// Specs are the specs the category asks for.
	Specs         []SpecNames
	CommuneExists bool
	CommunePoint  Point // the chosen commune's point
}

// Listing is one item on sale. Its fields are its stored state.
type Listing struct {
	s ListingState
}

// NewListing checks d and starts the listing in processing. The first write
// ID is the id.
func NewListing(id, seller uuid.UUID, d ListingInput, refs Refs, now time.Time) (Listing, error) {
	l := Listing{s: ListingState{ID: id, SellerID: seller, Status: StatusProcessing, WriteID: id, CreatedAt: now}}
	if err := l.apply(d, refs, now); err != nil {
		return Listing{}, err
	}
	return l, nil
}

// apply checks d and sets the listing's content from it, keeping the keys of
// photos it already has.
func (l *Listing) apply(d ListingInput, refs Refs, now time.Time) error {
	e := ErrInvalidListing
	title := strings.TrimSpace(d.Title)
	if n := utf8.RuneCountInString(title); n < 1 || n > MaxTitle {
		e = e.WithField("title", apperr.Length(n, 1, MaxTitle), "1 to 70 characters")
	}
	if d.Price < MinPrice || d.Price > MaxPrice {
		e = e.WithField("price", apperr.OutOfRange, "10,000 to 1,000,000,000 đồng")
	}
	description := strings.TrimSpace(d.Description)
	if utf8.RuneCountInString(description) > MaxDescription {
		e = e.WithField("description", apperr.TooLong, "at most 1,500 characters")
	}
	if !refs.CategoryIsLeaf {
		e = e.WithField("category_code", apperr.NotAllowed, "a subcategory")
	}
	if !refs.CommuneExists {
		e = e.WithField("commune_code", apperr.Unknown, "a commune of the administrative list")
	}
	if d.Condition != ConditionNew && d.Condition != ConditionLikeNew && d.Condition != ConditionUsed {
		e = e.WithField("condition", apperr.NotAllowed, "new, like_new or used")
	}
	if code := apperr.Count(len(d.Photos), 1, MaxPhotos); code != "" {
		e = e.WithField("photos", code, "1 to 10 distinct photos")
	} else if duplicates(d) {
		e = e.WithField("photos", apperr.Duplicate, "1 to 10 distinct photos")
	}
	if len(d.Specs) > MaxSpecs {
		e = e.WithField("specs", apperr.TooMany, "at most 20")
	}
	specs := KeySpecs(d.Specs, refs.Specs)
	keyed := map[string]bool{}
	for _, s := range specs {
		switch {
		case s.Key != "" && !slices.ContainsFunc(refs.Specs, func(n SpecNames) bool { return n.Key == s.Key }):
			e = e.WithField("specs", apperr.Unknown, "keys of the category's specs")
		case s.Key != "" && keyed[s.Key]:
			e = e.WithField("specs", apperr.Duplicate, "one spec of each key")
		case !within(s.Label, MaxSpecText) || !within(s.Value, MaxSpecText):
			code := apperr.TooLong
			if s.Label == "" || s.Value == "" {
				code = apperr.Required
			}
			e = e.WithField("specs", code, "a key or a label, and a value, 1 to 50 characters each")
		default:
		}
		keyed[s.Key] = true
	}
	if keys, labels := missingSpecs(refs.Specs, specs); len(keys) > 0 {
		e = e.WithField("specs", apperr.MissingSpecs, "a value for "+strings.Join(labels, ", ")).WithMissingSpecs(keys)
	}
	if d.Spot != nil && Distance(*d.Spot, refs.CommunePoint) > MaxGPSKm {
		e = e.WithField("gps", apperr.OutOfRange, "within 30 km of the commune")
	}
	if len(e.Fields) > 0 {
		return e
	}
	photos := make([]Photo, len(d.Photos))
	for i, id := range d.Photos {
		photos[i] = Photo{UploadID: id, Keys: l.keysOf(id)}
	}
	if d.Spot != nil {
		l.setPoint(*d.Spot)
	} else if l.Point() == (Point{}) || l.s.CommuneCode != d.CommuneCode {
		l.setPoint(refs.CommunePoint)
	}
	l.s.Title, l.s.Price, l.s.Description = title, d.Price, description
	l.s.CategoryCode, l.s.Condition, l.s.AllowOffers, l.s.CommuneCode = d.CategoryCode, d.Condition, d.AllowOffers, d.CommuneCode
	l.s.Photos, l.s.Specs, l.s.UpdatedAt = photos, specs, now
	return nil
}

// KeySpecs trims specs and gives each that names a spec of names, by its key
// or by its name in any language (case aside), that spec's key and its
// Vietnamese name as the label: the specs as a listing stores them. A key
// names lacks stays, for the caller to refuse.
func KeySpecs(specs []Spec, names []SpecNames) []Spec {
	out := make([]Spec, len(specs))
	for i, s := range specs {
		s = Spec{Key: strings.TrimSpace(s.Key), Label: strings.TrimSpace(s.Label), Value: strings.TrimSpace(s.Value)}
		j := slices.IndexFunc(names, func(n SpecNames) bool {
			if s.Key != "" {
				return n.Key == s.Key
			}
			for _, name := range n.Names {
				if strings.EqualFold(name, s.Label) {
					return true
				}
			}
			return false
		})
		if j >= 0 {
			s.Key, s.Label = names[j].Key, names[j].Names[locale.Vietnamese]
		}
		out[i] = s
	}
	return out
}

// missingSpecs are the keys and Vietnamese names of the specs of names that
// no spec of specs gives a value.
func missingSpecs(names []SpecNames, specs []Spec) (keys, labels []string) {
	for _, n := range names {
		if !slices.ContainsFunc(specs, func(s Spec) bool { return s.Key == n.Key && s.Value != "" }) {
			keys, labels = append(keys, n.Key), append(labels, n.Names[locale.Vietnamese])
		}
	}
	return keys, labels
}

func within(s string, maxRunes int) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= maxRunes
}

// duplicates reports an upload used twice in d.
func duplicates(d ListingInput) bool {
	seen := map[uuid.UUID]bool{}
	for _, id := range d.Photos {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

// keysOf returns the keys of upload id when the listing already holds it
// processed.
func (l *Listing) keysOf(id uuid.UUID) PhotoKeys {
	for _, p := range l.s.Photos {
		if p.UploadID == id {
			return p.Keys
		}
	}
	return PhotoKeys{}
}

func (l *Listing) ID() uuid.UUID       { return l.s.ID }
func (l *Listing) SellerID() uuid.UUID { return l.s.SellerID }
func (l *Listing) Title() string       { return l.s.Title }

// Slug is the last part of the listing's web address: Slug(title, code).
func (l *Listing) Slug() string         { return Slug(l.s.Title, l.s.Code) }
func (l *Listing) Price() int64         { return l.s.Price }
func (l *Listing) Description() string  { return l.s.Description }
func (l *Listing) CategoryCode() string { return l.s.CategoryCode }
func (l *Listing) Condition() Condition { return l.s.Condition }
func (l *Listing) AllowOffers() bool    { return l.s.AllowOffers }
func (l *Listing) CommuneCode() string  { return l.s.CommuneCode }

// Point is where distance is measured from. Server only: no view carries it.
func (l *Listing) Point() Point                    { return Point{Lat: l.s.Lat, Lng: l.s.Lng} }
func (l *Listing) Photos() []Photo                 { return append([]Photo{}, l.s.Photos...) }
func (l *Listing) Specs() []Spec                   { return append([]Spec{}, l.s.Specs...) }
func (l *Listing) Status() Status                  { return l.s.Status }
func (l *Listing) Rejection() map[uuid.UUID]string { return l.s.Rejection }
func (l *Listing) ReservedFor() uuid.UUID          { return l.s.ReservedFor }
func (l *Listing) WriteID() uuid.UUID              { return l.s.WriteID }
func (l *Listing) CreatedAt() time.Time            { return l.s.CreatedAt }
func (l *Listing) UpdatedAt() time.Time            { return l.s.UpdatedAt }
func (l *Listing) PublishedAt() *time.Time         { return l.s.PublishedAt }
func (l *Listing) SoldAt() *time.Time              { return l.s.SoldAt }
func (l *Listing) SoldOrder() *uuid.UUID           { return l.s.SoldOrder }
func (l *Listing) HeldAt() *time.Time              { return l.s.HeldAt }

// Uploads returns the upload IDs of the listing's photos.
func (l *Listing) Uploads() []uuid.UUID {
	out := make([]uuid.UUID, len(l.s.Photos))
	for i, p := range l.s.Photos {
		out[i] = p.UploadID
	}
	return out
}

// DeletedUploads returns the uploads a deleted listing no longer shows:
// every photo but a processed cover, which its orders, deals and chats
// still show. Any other listing shows them all.
func (l *Listing) DeletedUploads() []uuid.UUID {
	if l.s.Status != StatusDeleted {
		return nil
	}
	ids := l.Uploads()
	if len(l.s.Photos) > 0 && l.s.Photos[0].Keys.Photo != "" {
		return ids[1:]
	}
	return ids
}

// PendingUploads returns the uploads of the photos not processed yet.
func (l *Listing) PendingUploads() []uuid.UUID {
	var out []uuid.UUID
	for _, p := range l.s.Photos {
		if p.Keys.Photo == "" {
			out = append(out, p.UploadID)
		}
	}
	return out
}

// Edit replaces the listing's content with d. An upload not processed yet,
// or an edit of a rejected listing, sends the listing back through
// processing. Sold, deleted and reserved listings cannot be edited. A new
// price on a published listing is kept for PriceDrop.
func (l *Listing) Edit(d ListingInput, refs Refs, now time.Time) error {
	if l.s.Status == StatusSold || l.s.Status == StatusDeleted || l.s.Status == StatusReserved {
		return ErrStateConflict
	}
	was := l.s.Price
	if err := l.apply(d, refs, now); err != nil {
		return err
	}
	if l.s.PublishedAt != nil && l.s.Price != was {
		l.repriced(was, now)
	}
	if len(l.PendingUploads()) > 0 || l.s.Status == StatusRejected {
		l.s.Status, l.s.Rejection = StatusProcessing, nil
	}
	return nil
}

// Reprice sets the price alone, as an edit that changes only the price
// does, and reports whether it changed.
func (l *Listing) Reprice(price int64, now time.Time) (bool, error) {
	if l.s.Status == StatusSold || l.s.Status == StatusDeleted || l.s.Status == StatusReserved {
		return false, ErrStateConflict
	}
	if price < MinPrice || price > MaxPrice {
		return false, ErrInvalidListing.WithField("price", apperr.OutOfRange, "10,000 to 1,000,000,000 đồng")
	}
	if price == l.s.Price {
		return false, nil
	}
	was := l.s.Price
	l.s.Price, l.s.UpdatedAt = price, now
	if l.s.PublishedAt != nil {
		l.repriced(was, now)
	}
	return true, nil
}

// Published records the keys of the processed uploads and puts the listing
// on sale, or hidden while a member holds it. It reports whether this is
// the listing's first publication.
func (l *Listing) Published(keys map[uuid.UUID]PhotoKeys, now time.Time) (first bool) {
	for i := range l.s.Photos {
		if k, ok := keys[l.s.Photos[i].UploadID]; ok {
			l.s.Photos[i].Keys = k
		}
	}
	l.s.Status, l.s.Rejection, l.s.UpdatedAt = l.onSale(), nil, now
	if l.s.PublishedAt == nil {
		l.s.PublishedAt = &now
		return true
	}
	return false
}

// Rejected records why processing refused uploads; the seller edits the
// listing to try again.
func (l *Listing) Rejected(reasons map[uuid.UUID]string, now time.Time) {
	l.s.Status, l.s.Rejection, l.s.UpdatedAt = StatusRejected, reasons, now
}

func (l *Listing) Hide(now time.Time) error { return l.move(StatusActive, StatusHidden, now) }

// Unhide puts the seller's hidden listing back on sale; one a console
// member holds is ErrHeld.
func (l *Listing) Unhide(now time.Time) error {
	if l.s.HeldAt != nil {
		return ErrHeld
	}
	return l.move(StatusHidden, StatusActive, now)
}

// Hold is a console member hiding an active or hidden listing until Lift;
// the seller cannot unhide it meanwhile. A held listing is ErrStateConflict.
func (l *Listing) Hold(now time.Time) error {
	if l.s.HeldAt != nil || (l.s.Status != StatusActive && l.s.Status != StatusHidden) {
		return ErrStateConflict
	}
	l.s.Status, l.s.HeldAt, l.s.UpdatedAt = StatusHidden, &now, now
	return nil
}

// Lift ends a member's hold, and puts a listing still hidden back on sale.
// A listing nobody holds is ErrStateConflict.
func (l *Listing) Lift(now time.Time) error {
	if l.s.HeldAt == nil {
		return ErrStateConflict
	}
	if l.s.Status == StatusHidden {
		l.s.Status = StatusActive
	}
	l.s.HeldAt, l.s.UpdatedAt = nil, now
	return nil
}

// onSale is the status a listing goes back on sale with: hidden while a
// member holds it.
func (l *Listing) onSale() Status {
	if l.s.HeldAt != nil {
		return StatusHidden
	}
	return StatusActive
}

// MarkSold records a sale made off the app, of an active or hidden listing.
func (l *Listing) MarkSold(now time.Time) error {
	if l.s.Status != StatusActive && l.s.Status != StatusHidden {
		return ErrStateConflict
	}
	l.s.Status, l.s.SoldAt, l.s.UpdatedAt = StatusSold, &now, now
	return nil
}

// UnmarkSold undoes MarkSold; the listing is on sale again, or hidden while
// a member holds it. A listing sold through an order stays sold.
func (l *Listing) UnmarkSold(now time.Time) error {
	if l.s.SoldOrder != nil {
		return ErrStateConflict
	}
	if err := l.move(StatusSold, l.onSale(), now); err != nil {
		return err
	}
	l.s.SoldAt = nil
	return nil
}

// Delete removes the listing from every view but the database.
func (l *Listing) Delete(now time.Time) error {
	if l.s.Status == StatusDeleted || l.s.Status == StatusReserved {
		return ErrStateConflict
	}
	l.s.Status, l.s.DeletedAt, l.s.UpdatedAt = StatusDeleted, &now, now
	return nil
}

// Reserve holds an active listing for an order. Holding it again for the
// same order changes nothing and reports false.
func (l *Listing) Reserve(order uuid.UUID, now time.Time) (bool, error) {
	if l.s.Status == StatusReserved && l.s.ReservedFor == order {
		return false, nil
	}
	if l.s.Status != StatusActive {
		return false, ErrStateConflict
	}
	l.s.Status, l.s.ReservedFor, l.s.UpdatedAt = StatusReserved, order, now
	return true, nil
}

// Release puts a listing held for order back on sale and reports whether
// it did; a listing held for another order, or not held, stays as it is.
func (l *Listing) Release(order uuid.UUID, now time.Time) bool {
	if l.s.Status != StatusReserved || l.s.ReservedFor != order {
		return false
	}
	l.s.Status, l.s.ReservedFor, l.s.UpdatedAt = StatusActive, uuid.UUID{}, now
	return true
}

// SellTo marks a listing held for order sold, recording the order, and
// reports whether it did.
func (l *Listing) SellTo(order uuid.UUID, now time.Time) bool {
	if l.s.Status != StatusReserved || l.s.ReservedFor != order {
		return false
	}
	l.s.Status, l.s.ReservedFor, l.s.SoldOrder, l.s.SoldAt, l.s.UpdatedAt = StatusSold, uuid.UUID{}, &order, &now, now
	return true
}

func (l *Listing) move(from, to Status, now time.Time) error {
	if l.s.Status != from {
		return ErrStateConflict
	}
	l.s.Status, l.s.UpdatedAt = to, now
	return nil
}

// ListingState is every stored field of a listing. Stores only.
type ListingState struct {
	ID           uuid.UUID            `json:"id"`
	SellerID     uuid.UUID            `json:"seller_id"`
	Title        string               `json:"title"`
	Price        int64                `json:"price"`
	Description  string               `json:"description"`
	CategoryCode string               `json:"category_code"`
	Condition    Condition            `json:"condition"`
	AllowOffers  bool                 `json:"allow_offers"`
	CommuneCode  string               `json:"commune_code"`
	Photos       []Photo              `json:"photos"`
	Specs        []Spec               `json:"specs"`
	Status       Status               `json:"status"`
	Rejection    map[uuid.UUID]string `json:"rejection,omitempty"`
	ReservedFor  uuid.UUID            `json:"reserved_for"`
	SoldOrder    *uuid.UUID           `json:"sold_order,omitempty"`
	WriteID      uuid.UUID            `json:"write_id"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
	PublishedAt  *time.Time           `json:"published_at,omitempty"`
	SoldAt       *time.Time           `json:"sold_at,omitempty"`
	DeletedAt    *time.Time           `json:"deleted_at,omitempty"`
	HeldAt       *time.Time           `json:"held_at,omitempty"`
	Lat          float64              `json:"lat"`
	Lng          float64              `json:"lng"`
	Code         string               `json:"code,omitempty"`
	// PreviousPrice is 0 when PriceChangedAt is nil.
	PreviousPrice  int64      `json:"previous_price,omitempty"`
	PriceChangedAt *time.Time `json:"price_changed_at,omitempty"`
}

// RestoreListing rebuilds a stored listing. Stores only.
func RestoreListing(st ListingState) Listing { return Listing{s: st} }

// State returns every stored field. Stores only.
func (l *Listing) State() ListingState { return l.s }

func (l *Listing) setPoint(p Point) { l.s.Lat, l.s.Lng = p.Lat, p.Lng }

// Saved records that a store wrote l under writeID. Stores only.
func (l *Listing) Saved(writeID uuid.UUID) { l.s.WriteID = writeID }

// MarshalJSON lets a Restate step return a listing for its journal.
func (l Listing) MarshalJSON() ([]byte, error) { return json.Marshal(l.State()) }

func (l *Listing) UnmarshalJSON(b []byte) error {
	var st ListingState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	*l = RestoreListing(st)
	return nil
}
