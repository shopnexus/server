package domain

import (
	"slices"
	"time"
	"uuid"
)

const (
	// MaxDrafts is how many drafts an account keeps on the server.
	MaxDrafts = 20
	// DraftLife is how long a draft lives after its last write.
	DraftLife = 30 * 24 * time.Hour
)

// DraftContent is what the app sends of a draft. The server reads Title,
// Price and Photos; Body is the app's JSON, laid out as SchemaVersion says,
// and the server keeps it without reading it.
type DraftContent struct {
	SchemaVersion int
	Title         string
	Price         *int64
	Photos        []uuid.UUID
	Body          string
}

// ListingDraft is a listing its seller started and has not posted, kept
// for their other devices. The last write wins.
type ListingDraft struct {
	s ListingDraftState
}

// NewListingDraft starts account's draft id.
func NewListingDraft(id, account uuid.UUID, c DraftContent, now time.Time) ListingDraft {
	d := ListingDraft{s: ListingDraftState{ID: id, AccountID: account, CreatedAt: now}}
	d.Replace(c, now)
	return d
}

// Replace writes c over the draft, which then lives DraftLife from now.
func (d *ListingDraft) Replace(c DraftContent, now time.Time) {
	c.Photos = KeepPhotos(c.Photos, nil)
	d.s.Content, d.s.UpdatedAt, d.s.ExpiresAt = c, now, now.Add(DraftLife)
}

func (d *ListingDraft) ID() uuid.UUID        { return d.s.ID }
func (d *ListingDraft) AccountID() uuid.UUID { return d.s.AccountID }
func (d *ListingDraft) Photos() []uuid.UUID  { return slices.Clone(d.s.Content.Photos) }
func (d *ListingDraft) CreatedAt() time.Time { return d.s.CreatedAt }
func (d *ListingDraft) UpdatedAt() time.Time { return d.s.UpdatedAt }
func (d *ListingDraft) ExpiresAt() time.Time { return d.s.ExpiresAt }
func (d *ListingDraft) Content() DraftContent {
	c := d.s.Content
	c.Photos = slices.Clone(c.Photos)
	return c
}

// CheckPut decides whether account may write the draft whose stored row
// is stored, nil when there is none, while holding count drafts. Another
// account's draft answers as none would, so an ID tells nothing; only a
// create needs a free place.
func CheckPut(stored *ListingDraft, account uuid.UUID, count int) error {
	switch {
	case stored != nil && stored.s.AccountID != account:
		return ErrDraftNotFound
	case stored == nil && count >= MaxDrafts:
		return ErrDraftLimitReached
	}
	return nil
}

// KeepPhotos returns photos in order, each once, without those in gone.
func KeepPhotos(photos, gone []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(photos))
	for _, p := range photos {
		if !slices.Contains(gone, p) && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// ListingDraftState is every stored field of a draft. Stores only.
type ListingDraftState struct {
	ID, AccountID                   uuid.UUID
	Content                         DraftContent
	CreatedAt, UpdatedAt, ExpiresAt time.Time
}

// RestoreListingDraft rebuilds a stored draft. Stores only.
func RestoreListingDraft(st ListingDraftState) ListingDraft { return ListingDraft{s: st} }

// State returns every stored field. Stores only.
func (d *ListingDraft) State() ListingDraftState {
	st := d.s
	st.Content = d.Content()
	return st
}
