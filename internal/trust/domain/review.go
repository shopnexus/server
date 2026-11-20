// Package domain holds module trust's entities: the reviews the two sides
// of a closed order give each other, and the reports people make of
// listings and accounts.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

// Role is the side of the order the subject of a review was on.
type Role string

const (
	// RoleSeller rates the person who sold; the buyer writes it.
	RoleSeller Role = "seller"
	// RoleBuyer rates the person who bought; the seller writes it.
	RoleBuyer Role = "buyer"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == RoleSeller || r == RoleBuyer }

// Other is the role of the review the other side of the order writes.
func (r Role) Other() Role {
	if r == RoleSeller {
		return RoleBuyer
	}
	return RoleSeller
}

const (
	MaxStars  = 5
	MaxText   = 500
	MaxPhotos = 4
	// EditWindow is how long after the first submission a review may be
	// edited or deleted.
	EditWindow = 7 * 24 * time.Hour
	// BlindWindow is how long after the order closed a review stays
	// hidden when the other side has not reviewed.
	BlindWindow = 7 * 24 * time.Hour
)

// RevealAt is when a new review becomes visible: now when the other side
// has reviewed the order, otherwise BlindWindow after it closed.
func RevealAt(closedAt time.Time, otherSide bool, now time.Time) time.Time {
	at := closedAt.Add(BlindWindow)
	if otherSide || at.Before(now) {
		return now
	}
	return at
}

// Photo is one processed photo of a review, by the upload it came from.
type Photo struct {
	UploadID uuid.UUID `json:"upload_id"`
	PhotoKey string    `json:"photo_key"`
	ThumbKey string    `json:"thumb_key"`
}

// Review is one side's stars, words and photos about the other side of an
// order, and the subject's one reply. A deleted review keeps its first
// submission time and loses its text and photos.
type Review struct {
	id         uuid.UUID
	orderID    uuid.UUID
	role       Role
	reviewerID uuid.UUID
	subjectID  uuid.UUID
	listingID  uuid.UUID
	stars      int
	text       string
	photos     []Photo
	reply      string
	repliedAt  *time.Time
	visibleAt  time.Time
	deletedAt  *time.Time
	writeID    uuid.UUID
	createdAt  time.Time
	updatedAt  time.Time
}

// NewReview checks a first review: 1 to 5 stars, up to 500 characters
// of text after trimming and up to 4 photos. visibleAt comes from RevealAt.
func NewReview(id, order uuid.UUID, role Role, reviewer, subject, listing uuid.UUID, stars int, text string, photos []Photo,
	visibleAt, now time.Time,
) (Review, error) {
	r := Review{
		id: id, orderID: order, role: role, reviewerID: reviewer, subjectID: subject, listingID: listing,
		visibleAt: visibleAt, writeID: id, createdAt: now,
	}
	if err := r.set(stars, text, photos, now); err != nil {
		return Review{}, err
	}
	return r, nil
}

// CanChange is ErrClosed once the subject replied, and ErrLocked once the
// review shows or EditWindow has passed since the first submission: a side
// that read the other's stars cannot change its own.
func (r *Review) CanChange(now time.Time) error {
	if r.repliedAt != nil {
		return ErrClosed
	}
	if r.Visible(now) || now.Sub(r.createdAt) > EditWindow {
		return ErrLocked
	}
	return nil
}

// ChangeUntil is the last moment CanChange allows, short of a reply: when
// the review shows, or the end of EditWindow if that comes first.
func (r *Review) ChangeUntil() time.Time {
	if end := r.createdAt.Add(EditWindow); end.Before(r.visibleAt) {
		return end
	}
	return r.visibleAt
}

// Edit replaces the stars, the text and the photos while CanChange allows;
// a deleted review is posted again.
func (r *Review) Edit(stars int, text string, photos []Photo, now time.Time) error {
	if err := r.CanChange(now); err != nil {
		return err
	}
	if err := r.set(stars, text, photos, now); err != nil {
		return err
	}
	r.deletedAt = nil
	return nil
}

// Delete takes the review down while CanChange allows and drops its text
// and photos.
func (r *Review) Delete(now time.Time) error {
	if err := r.CanChange(now); err != nil {
		return err
	}
	r.deletedAt, r.text, r.photos, r.updatedAt = &now, "", nil, now
	return nil
}

// Reply is the subject's one answer, up to 500 characters, to a review
// they can see. Anyone else gets ErrNotFound, so a hidden review does not
// leak. After it the review is closed.
func (r *Review) Reply(by uuid.UUID, text string, now time.Time) error {
	if by != r.subjectID || r.Deleted() {
		return ErrNotFound
	}
	if !r.Visible(now) {
		return ErrHidden
	}
	if r.repliedAt != nil {
		return ErrReplied
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxText {
		return ErrInvalidReview.WithField("text", "1 to 500 characters")
	}
	r.reply, r.repliedAt = text, &now
	return nil
}

// Visible reports whether the subject and the public see the review at
// now; its reviewer always does.
func (r *Review) Visible(now time.Time) bool { return !r.visibleAt.After(now) }

// Deleted reports whether the reviewer took the review down.
func (r *Review) Deleted() bool { return r.deletedAt != nil }

// Replied reports whether the subject replied.
func (r *Review) Replied() bool { return r.repliedAt != nil }

func (r *Review) set(stars int, text string, photos []Photo, now time.Time) error {
	e := ErrInvalidReview
	text = strings.TrimSpace(text)
	if stars < 1 || stars > MaxStars {
		e = e.WithField("stars", "1 to 5")
	}
	if utf8.RuneCountInString(text) > MaxText {
		e = e.WithField("text", "at most 500 characters")
	}
	if len(photos) > MaxPhotos {
		e = e.WithField("photos", "at most 4")
	}
	if len(e.Fields) > 0 {
		return e
	}
	r.stars, r.text, r.photos, r.updatedAt = stars, text, photos, now
	return nil
}

func (r *Review) ID() uuid.UUID         { return r.id }
func (r *Review) OrderID() uuid.UUID    { return r.orderID }
func (r *Review) Role() Role            { return r.role }
func (r *Review) ReviewerID() uuid.UUID { return r.reviewerID }
func (r *Review) SubjectID() uuid.UUID  { return r.subjectID }
func (r *Review) ListingID() uuid.UUID  { return r.listingID }
func (r *Review) Stars() int            { return r.stars }
func (r *Review) Text() string          { return r.text }
func (r *Review) Photos() []Photo       { return r.photos }
func (r *Review) ReplyText() string     { return r.reply }
func (r *Review) RepliedAt() *time.Time { return r.repliedAt }
func (r *Review) VisibleAt() time.Time  { return r.visibleAt }
func (r *Review) CreatedAt() time.Time  { return r.createdAt }
func (r *Review) UpdatedAt() time.Time  { return r.updatedAt }
func (r *Review) WriteID() uuid.UUID    { return r.writeID }

// ReviewState is every stored field of a review. Stores only.
type ReviewState struct {
	ID         uuid.UUID
	OrderID    uuid.UUID
	Role       Role
	ReviewerID uuid.UUID
	SubjectID  uuid.UUID
	ListingID  uuid.UUID
	Stars      int
	Text       string
	Photos     []Photo
	Reply      string
	RepliedAt  *time.Time
	VisibleAt  time.Time
	DeletedAt  *time.Time
	WriteID    uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// RestoreReview rebuilds a stored review. Stores only.
func RestoreReview(st ReviewState) Review {
	return Review{
		id: st.ID, orderID: st.OrderID, role: st.Role, reviewerID: st.ReviewerID, subjectID: st.SubjectID,
		listingID: st.ListingID, stars: st.Stars, text: st.Text, photos: st.Photos, reply: st.Reply, repliedAt: st.RepliedAt,
		visibleAt: st.VisibleAt, deletedAt: st.DeletedAt, writeID: st.WriteID, createdAt: st.CreatedAt,
		updatedAt: st.UpdatedAt,
	}
}

// State returns every stored field. Stores only.
func (r *Review) State() ReviewState {
	return ReviewState{
		ID: r.id, OrderID: r.orderID, Role: r.role, ReviewerID: r.reviewerID, SubjectID: r.subjectID,
		ListingID: r.listingID, Stars: r.stars, Text: r.text, Photos: r.photos, Reply: r.reply, RepliedAt: r.repliedAt,
		VisibleAt: r.visibleAt, DeletedAt: r.deletedAt, WriteID: r.writeID, CreatedAt: r.createdAt,
		UpdatedAt: r.updatedAt,
	}
}

// Saved records that a store wrote r under writeID. Stores only.
func (r *Review) Saved(writeID uuid.UUID) { r.writeID = writeID }
