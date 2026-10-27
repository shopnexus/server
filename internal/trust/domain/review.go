// Package domain holds module trust's entities: for now the review a buyer
// gives the seller of a shipped order.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

const (
	MaxStars = 5
	MaxText  = 500
	// EditWindow is how long after the first submission a review may be
	// edited or deleted.
	EditWindow = 7 * 24 * time.Hour
)

// Review is a buyer's stars and words about the seller of one order. A
// deleted review keeps its first submission time and loses its text.
type Review struct {
	id        uuid.UUID
	orderID   uuid.UUID
	sellerID  uuid.UUID
	buyerID   uuid.UUID
	listingID uuid.UUID
	stars     int
	text      string
	deletedAt *time.Time
	writeID   uuid.UUID
	createdAt time.Time
	updatedAt time.Time
}

// NewReview checks a first review: 1 to 5 stars and up to 500 characters
// of text after trimming.
func NewReview(id, order, seller, buyer, listing uuid.UUID, stars int, text string, now time.Time) (Review, error) {
	r := Review{id: id, orderID: order, sellerID: seller, buyerID: buyer, listingID: listing, writeID: id, createdAt: now}
	if err := r.set(stars, text, now); err != nil {
		return Review{}, err
	}
	return r, nil
}

// CanChange is ErrLocked once EditWindow has passed since the first
// submission.
func (r *Review) CanChange(now time.Time) error {
	if now.Sub(r.createdAt) > EditWindow {
		return ErrLocked
	}
	return nil
}

// Edit replaces the stars and the text within EditWindow; a deleted
// review is posted again.
func (r *Review) Edit(stars int, text string, now time.Time) error {
	if err := r.CanChange(now); err != nil {
		return err
	}
	if err := r.set(stars, text, now); err != nil {
		return err
	}
	r.deletedAt = nil
	return nil
}

// Delete takes the review down within EditWindow and drops its text.
func (r *Review) Delete(now time.Time) error {
	if err := r.CanChange(now); err != nil {
		return err
	}
	r.deletedAt, r.text, r.updatedAt = &now, "", now
	return nil
}

// Deleted reports whether the buyer took the review down.
func (r *Review) Deleted() bool { return r.deletedAt != nil }

func (r *Review) set(stars int, text string, now time.Time) error {
	e := ErrInvalidReview
	text = strings.TrimSpace(text)
	if stars < 1 || stars > MaxStars {
		e = e.WithField("stars", "1 to 5")
	}
	if utf8.RuneCountInString(text) > MaxText {
		e = e.WithField("text", "at most 500 characters")
	}
	if len(e.Fields) > 0 {
		return e
	}
	r.stars, r.text, r.updatedAt = stars, text, now
	return nil
}

func (r *Review) ID() uuid.UUID        { return r.id }
func (r *Review) OrderID() uuid.UUID   { return r.orderID }
func (r *Review) SellerID() uuid.UUID  { return r.sellerID }
func (r *Review) BuyerID() uuid.UUID   { return r.buyerID }
func (r *Review) ListingID() uuid.UUID { return r.listingID }
func (r *Review) Stars() int           { return r.stars }
func (r *Review) Text() string         { return r.text }
func (r *Review) CreatedAt() time.Time { return r.createdAt }
func (r *Review) UpdatedAt() time.Time { return r.updatedAt }
func (r *Review) WriteID() uuid.UUID   { return r.writeID }

// ReviewState is every stored field of a review. Stores only.
type ReviewState struct {
	ID        uuid.UUID
	OrderID   uuid.UUID
	SellerID  uuid.UUID
	BuyerID   uuid.UUID
	ListingID uuid.UUID
	Stars     int
	Text      string
	DeletedAt *time.Time
	WriteID   uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RestoreReview rebuilds a stored review. Stores only.
func RestoreReview(st ReviewState) Review {
	return Review{
		id: st.ID, orderID: st.OrderID, sellerID: st.SellerID, buyerID: st.BuyerID, listingID: st.ListingID,
		stars: st.Stars, text: st.Text, deletedAt: st.DeletedAt, writeID: st.WriteID, createdAt: st.CreatedAt,
		updatedAt: st.UpdatedAt,
	}
}

// State returns every stored field. Stores only.
func (r *Review) State() ReviewState {
	return ReviewState{
		ID: r.id, OrderID: r.orderID, SellerID: r.sellerID, BuyerID: r.buyerID, ListingID: r.listingID,
		Stars: r.stars, Text: r.text, DeletedAt: r.deletedAt, WriteID: r.writeID, CreatedAt: r.createdAt,
		UpdatedAt: r.updatedAt,
	}
}

// Saved records that a store wrote r under writeID. Stores only.
func (r *Review) Saved(writeID uuid.UUID) { r.writeID = writeID }
