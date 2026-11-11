// Package repository stores module assist's reads in Postgres. Each method
// is its own transaction.
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
	// DeleteTranscript removes what PutTranscript kept under account and
	// requestKey; an absent one is fine.
	DeleteTranscript(ctx context.Context, account uuid.UUID, requestKey string) error
	// Delete removes a read; an absent one is fine.
	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteAccount removes every read of the account and its waiting
	// transcripts.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
}
