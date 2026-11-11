// Package repository stores module assist's video reads and link reads in
// Postgres. Each method is its own transaction.
package repository

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
)

// ErrConflict is a save that lost a race: the row changed meanwhile.
var ErrConflict = errors.New("assist: changed meanwhile")

// VideoReads persists video reads.
type VideoReads interface {
	// Insert stores a new read and notifies its account. It takes a share
	// lock on the account's row, so a deleted account gets
	// accountdomain.ErrSessionRevoked, and counts the account's reads of the
	// last domain.ReadWindow before r's start one insert at a time:
	// domain.ReadLimit of them is domain.ErrReadLimit. A read already
	// stored passes without a count or a notice, so a retried step
	// succeeds.
	Insert(ctx context.Context, r domain.VideoRead) error
	// Save writes r as writeID when the row still has r's write ID, notifies
	// the account and records writeID on r. A row already at writeID passes
	// without a notice; any other row is ErrConflict.
	Save(ctx context.Context, r *domain.VideoRead, writeID uuid.UUID) error
	// Get returns domain.ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.VideoRead, error)
	// Delete removes a read; an absent one is fine.
	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteAccount removes every read of the account.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
}

// LinkReads persists link reads, with the rules of VideoReads: Insert
// counts the account's link reads of the last domain.ReadWindow under a
// lock (domain.ReadLimit is domain.ErrReadLimit, a deleted account
// accountdomain.ErrSessionRevoked) and notifies; Save writes under a write
// ID and notifies, ErrConflict on a lost race; Get returns
// domain.ErrLinkReadNotFound for an unknown id.
type LinkReads interface {
	Insert(ctx context.Context, r domain.LinkRead) error
	Save(ctx context.Context, r *domain.LinkRead, writeID uuid.UUID) error
	Get(ctx context.Context, id uuid.UUID) (domain.LinkRead, error)
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteAccount(ctx context.Context, account uuid.UUID) error
}

// ListingReads persists reads of every source.
type ListingReads interface {
	// Insert stores a new read and notifies its account. It takes a share
	// lock on the account's row, so a deleted account gets
	// accountdomain.ErrSessionRevoked, and counts the account's reads of
	// r's source in the last domain.ReadWindow before r's start one insert
	// at a time: r.Source().Cap() of them is domain.ErrReadLimit. A video
	// read takes in the transcript waiting under its account and
	// requestKey and deletes it; every insert deletes the waiting
	// transcripts older than domain.TranscriptWait. A read already stored
	// passes without a count or a notice, so a retried step succeeds.
	Insert(ctx context.Context, r domain.ListingRead, requestKey string) error
	// Save writes r as writeID when the row still has r's write ID, notifies
	// the account and records writeID on r. An ended read's transcript
	// becomes NULL. A row already at writeID passes without a notice; any
	// other row is ErrConflict.
	Save(ctx context.Context, r *domain.ListingRead, writeID uuid.UUID) error
	// Get returns domain.ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.ListingRead, error)
	// Transcript is the phone's transcript of a read still reading; "" when
	// there is none. An unknown id is domain.ErrNotFound.
	Transcript(ctx context.Context, id uuid.UUID) (string, error)
	// PutTranscript keeps text for the video read account starts under
	// requestKey, until Insert takes it in or domain.TranscriptWait passes.
	// A second put under the same key replaces the first.
	PutTranscript(ctx context.Context, account uuid.UUID, requestKey, text string, at time.Time) error
	// Delete removes a read; an absent one is fine.
	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteAccount removes every read of the account and its waiting
	// transcripts.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
}
