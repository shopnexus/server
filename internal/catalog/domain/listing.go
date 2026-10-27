// Package domain holds module catalog's entities: the listing, one item a
// seller offers at one price, with its photos, declared flaws and specs,
// and the categories and regions it is filed under.
package domain

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

const (
	// MaxOnSale is how many listings a seller may have on sale or hidden.
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
}

// Photo is an upload of the listing; Keys is empty until it is processed.
type Photo struct {
	UploadID uuid.UUID `json:"upload_id"`
	Keys     PhotoKeys `json:"keys"`
}

type Spec struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Draft is a listing as the seller sends it, to post or to edit.
type Draft struct {
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
// draft.
type Refs struct {
	CategoryIsLeaf bool
	CommuneExists  bool
	CommunePoint   Point // the chosen commune's point
}

// Listing is one item on sale.
type Listing struct {
	id           uuid.UUID
	sellerID     uuid.UUID
	title        string
	price        int64
	description  string
	categoryCode string
	condition    Condition
	allowOffers  bool
	communeCode  string
	point        Point
	photos       []Photo
	specs        []Spec
	status       Status
	rejection    map[uuid.UUID]string
	reservedFor  uuid.UUID
	writeID      uuid.UUID
	createdAt    time.Time
	updatedAt    time.Time
	publishedAt  *time.Time
	soldAt       *time.Time
	deletedAt    *time.Time
}

// NewListing checks d and starts the listing in processing. The first write
// ID is the id.
func NewListing(id, seller uuid.UUID, d Draft, refs Refs, now time.Time) (Listing, error) {
	l := Listing{id: id, sellerID: seller, status: StatusProcessing, writeID: id, createdAt: now}
	if err := l.apply(d, refs, now); err != nil {
		return Listing{}, err
	}
	return l, nil
}

// apply checks d and sets the listing's content from it, keeping the keys of
// photos it already has.
func (l *Listing) apply(d Draft, refs Refs, now time.Time) error {
	e := ErrInvalidListing
	title := strings.TrimSpace(d.Title)
	if n := utf8.RuneCountInString(title); n < 1 || n > MaxTitle {
		e = e.WithField("title", "1 to 70 characters")
	}
	if d.Price < MinPrice || d.Price > MaxPrice {
		e = e.WithField("price", "10,000 to 1,000,000,000 đồng")
	}
	description := strings.TrimSpace(d.Description)
	if utf8.RuneCountInString(description) > MaxDescription {
		e = e.WithField("description", "at most 1,500 characters")
	}
	if !refs.CategoryIsLeaf {
		e = e.WithField("category_code", "a subcategory")
	}
	if !refs.CommuneExists {
		e = e.WithField("commune_code", "a commune of the administrative list")
	}
	if d.Condition != ConditionNew && d.Condition != ConditionLikeNew && d.Condition != ConditionUsed {
		e = e.WithField("condition", "new, like_new or used")
	}
	if len(d.Photos) < 1 || len(d.Photos) > MaxPhotos || duplicates(d) {
		e = e.WithField("photos", "1 to 10 distinct photos")
	}
	if len(d.Specs) > MaxSpecs {
		e = e.WithField("specs", "at most 20")
	}
	specs := make([]Spec, 0, len(d.Specs))
	for _, s := range d.Specs {
		label, value := strings.TrimSpace(s.Label), strings.TrimSpace(s.Value)
		if !within(label, MaxSpecText) || !within(value, MaxSpecText) {
			e = e.WithField("specs", "label and value 1 to 50 characters each")
		}
		specs = append(specs, Spec{Label: label, Value: value})
	}
	if d.Spot != nil && Distance(*d.Spot, refs.CommunePoint) > MaxGPSKm {
		e = e.WithField("gps", "within 30 km of the commune")
	}
	if len(e.Fields) > 0 {
		return e
	}
	photos := make([]Photo, len(d.Photos))
	for i, id := range d.Photos {
		photos[i] = Photo{UploadID: id, Keys: l.keysOf(id)}
	}
	if d.Spot != nil {
		l.point = *d.Spot
	} else if l.point == (Point{}) || l.communeCode != d.CommuneCode {
		l.point = refs.CommunePoint
	}
	l.title, l.price, l.description = title, d.Price, description
	l.categoryCode, l.condition, l.allowOffers, l.communeCode = d.CategoryCode, d.Condition, d.AllowOffers, d.CommuneCode
	l.photos, l.specs, l.updatedAt = photos, specs, now
	return nil
}

func within(s string, maxRunes int) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= maxRunes
}

// duplicates reports an upload used twice in d.
func duplicates(d Draft) bool {
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
	for _, p := range l.photos {
		if p.UploadID == id {
			return p.Keys
		}
	}
	return PhotoKeys{}
}

func (l *Listing) ID() uuid.UUID        { return l.id }
func (l *Listing) SellerID() uuid.UUID  { return l.sellerID }
func (l *Listing) Title() string        { return l.title }
func (l *Listing) Price() int64         { return l.price }
func (l *Listing) Description() string  { return l.description }
func (l *Listing) CategoryCode() string { return l.categoryCode }
func (l *Listing) Condition() Condition { return l.condition }
func (l *Listing) AllowOffers() bool    { return l.allowOffers }
func (l *Listing) CommuneCode() string  { return l.communeCode }

// Point is where distance is measured from. Server only: no view carries it.
func (l *Listing) Point() Point                    { return l.point }
func (l *Listing) Photos() []Photo                 { return append([]Photo{}, l.photos...) }
func (l *Listing) Specs() []Spec                   { return append([]Spec{}, l.specs...) }
func (l *Listing) Status() Status                  { return l.status }
func (l *Listing) Rejection() map[uuid.UUID]string { return l.rejection }
func (l *Listing) ReservedFor() uuid.UUID          { return l.reservedFor }
func (l *Listing) WriteID() uuid.UUID              { return l.writeID }
func (l *Listing) CreatedAt() time.Time            { return l.createdAt }
func (l *Listing) UpdatedAt() time.Time            { return l.updatedAt }
func (l *Listing) PublishedAt() *time.Time         { return l.publishedAt }
func (l *Listing) SoldAt() *time.Time              { return l.soldAt }

// Uploads returns the upload IDs of the listing's photos.
func (l *Listing) Uploads() []uuid.UUID {
	out := make([]uuid.UUID, len(l.photos))
	for i, p := range l.photos {
		out[i] = p.UploadID
	}
	return out
}

// PendingUploads returns the uploads of the photos not processed yet.
func (l *Listing) PendingUploads() []uuid.UUID {
	var out []uuid.UUID
	for _, p := range l.photos {
		if p.Keys.Photo == "" {
			out = append(out, p.UploadID)
		}
	}
	return out
}

// Edit replaces the listing's content with d. An upload not processed yet,
// or an edit of a rejected listing, sends the listing back through
// processing. Sold and deleted listings cannot be edited.
func (l *Listing) Edit(d Draft, refs Refs, now time.Time) error {
	if l.status == StatusSold || l.status == StatusDeleted || l.status == StatusReserved {
		return ErrStateConflict
	}
	if err := l.apply(d, refs, now); err != nil {
		return err
	}
	if len(l.PendingUploads()) > 0 || l.status == StatusRejected {
		l.status, l.rejection = StatusProcessing, nil
	}
	return nil
}

// Published records the keys of the processed uploads and puts the listing
// on sale.
func (l *Listing) Published(keys map[uuid.UUID]PhotoKeys, now time.Time) {
	for i := range l.photos {
		if k, ok := keys[l.photos[i].UploadID]; ok {
			l.photos[i].Keys = k
		}
	}
	l.status, l.rejection, l.updatedAt = StatusActive, nil, now
	if l.publishedAt == nil {
		l.publishedAt = &now
	}
}

// Rejected records why processing refused uploads; the seller edits the
// listing to try again.
func (l *Listing) Rejected(reasons map[uuid.UUID]string, now time.Time) {
	l.status, l.rejection, l.updatedAt = StatusRejected, reasons, now
}

func (l *Listing) Hide(now time.Time) error   { return l.move(StatusActive, StatusHidden, now) }
func (l *Listing) Unhide(now time.Time) error { return l.move(StatusHidden, StatusActive, now) }

// MarkSold records a sale made off the app, of an active or hidden listing.
func (l *Listing) MarkSold(now time.Time) error {
	if l.status != StatusActive && l.status != StatusHidden {
		return ErrStateConflict
	}
	l.status, l.soldAt, l.updatedAt = StatusSold, &now, now
	return nil
}

// UnmarkSold undoes MarkSold; the listing is on sale again.
func (l *Listing) UnmarkSold(now time.Time) error {
	if err := l.move(StatusSold, StatusActive, now); err != nil {
		return err
	}
	l.soldAt = nil
	return nil
}

// Delete removes the listing from every view but the database.
func (l *Listing) Delete(now time.Time) error {
	if l.status == StatusDeleted || l.status == StatusReserved {
		return ErrStateConflict
	}
	l.status, l.deletedAt, l.updatedAt = StatusDeleted, &now, now
	return nil
}

// Reserve holds an active listing for an order. Holding it again for the
// same order changes nothing and reports false.
func (l *Listing) Reserve(order uuid.UUID, now time.Time) (bool, error) {
	if l.status == StatusReserved && l.reservedFor == order {
		return false, nil
	}
	if l.status != StatusActive {
		return false, ErrStateConflict
	}
	l.status, l.reservedFor, l.updatedAt = StatusReserved, order, now
	return true, nil
}

// Release puts a listing held for order back on sale and reports whether
// it did; a listing held for another order, or not held, stays as it is.
func (l *Listing) Release(order uuid.UUID, now time.Time) bool {
	if l.status != StatusReserved || l.reservedFor != order {
		return false
	}
	l.status, l.reservedFor, l.updatedAt = StatusActive, uuid.UUID{}, now
	return true
}

// SellTo marks a listing held for order sold and reports whether it did.
func (l *Listing) SellTo(order uuid.UUID, now time.Time) bool {
	if l.status != StatusReserved || l.reservedFor != order {
		return false
	}
	l.status, l.reservedFor, l.soldAt, l.updatedAt = StatusSold, uuid.UUID{}, &now, now
	return true
}

func (l *Listing) move(from, to Status, now time.Time) error {
	if l.status != from {
		return ErrStateConflict
	}
	l.status, l.updatedAt = to, now
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
	WriteID      uuid.UUID            `json:"write_id"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
	PublishedAt  *time.Time           `json:"published_at,omitempty"`
	SoldAt       *time.Time           `json:"sold_at,omitempty"`
	DeletedAt    *time.Time           `json:"deleted_at,omitempty"`
	Lat          float64              `json:"lat"`
	Lng          float64              `json:"lng"`
}

// RestoreListing rebuilds a stored listing. Stores only.
func RestoreListing(st ListingState) Listing {
	return Listing{
		id: st.ID, sellerID: st.SellerID, title: st.Title, price: st.Price, description: st.Description,
		categoryCode: st.CategoryCode, condition: st.Condition, allowOffers: st.AllowOffers, communeCode: st.CommuneCode,
		photos: st.Photos, specs: st.Specs, status: st.Status, rejection: st.Rejection, reservedFor: st.ReservedFor,
		writeID: st.WriteID, createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
		publishedAt: st.PublishedAt, soldAt: st.SoldAt, deletedAt: st.DeletedAt, point: Point{Lat: st.Lat, Lng: st.Lng},
	}
}

// State returns every stored field. Stores only.
func (l *Listing) State() ListingState {
	return ListingState{
		ID: l.id, SellerID: l.sellerID, Title: l.title, Price: l.price, Description: l.description,
		CategoryCode: l.categoryCode, Condition: l.condition, AllowOffers: l.allowOffers, CommuneCode: l.communeCode,
		Photos: l.photos, Specs: l.specs, Status: l.status, Rejection: l.rejection,
		ReservedFor: l.reservedFor, WriteID: l.writeID, CreatedAt: l.createdAt, UpdatedAt: l.updatedAt,
		PublishedAt: l.publishedAt, SoldAt: l.soldAt, DeletedAt: l.deletedAt, Lat: l.point.Lat, Lng: l.point.Lng,
	}
}

// Saved records that a store wrote l under writeID. Stores only.
func (l *Listing) Saved(writeID uuid.UUID) { l.writeID = writeID }

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
