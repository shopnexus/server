// Package events is server's outbox. A repository queues an Event with
// Queue in the batch of the change it announces, and Bus hands it to the
// Restate handlers subscribed to its kind.
package events

import (
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/postgres"
)

// Kind is what happened, in the form aggregate.past_tense.
type Kind string

const (
	AccountUpdated     Kind = "account.updated"
	AccountSuspended   Kind = "account.suspended"
	AccountUnsuspended Kind = "account.unsuspended"
	// AccountDeleted is an account its person deleted; each module clears
	// what it keeps of the account.
	AccountDeleted   Kind = "account.deleted"
	ListingPublished Kind = "listing.published"
	ListingUpdated   Kind = "listing.updated"
	ListingHidden    Kind = "listing.hidden"
	ListingUnhidden  Kind = "listing.unhidden"
	ListingSold      Kind = "listing.sold"
	ListingUnsold    Kind = "listing.unsold"
	ListingDeleted   Kind = "listing.deleted"
	ListingReserved  Kind = "listing.reserved"
	ListingReleased  Kind = "listing.released"
	DealMoved        Kind = "deal.moved"
	// MessageSent is a chat message its recipient can now read.
	MessageSent Kind = "message.sent"
	// ListingRejected is a listing processing refused a photo of.
	ListingRejected Kind = "listing.rejected"
	// ReviewPosted is a review's first submission.
	ReviewPosted Kind = "review.posted"
	// ReviewReplied is the subject's one reply to a review.
	ReviewReplied Kind = "review.replied"
	// SavedPriceDropped is a saved listing's price at a new low below the
	// save's alert level; the payload's price is that price.
	SavedPriceDropped Kind = "saved.price_dropped"
	// SearchWatchMatched is a sweep that found new listings for a watch with
	// its alert on; the payload's count is the watch's new_count after it.
	SearchWatchMatched Kind = "search_watch.matched"
	// FeedbackMoved is a feedback whose status staff moved to in_progress
	// or shipped; FeedbackReplied is ShopNexus's first reply to one.
	FeedbackMoved   Kind = "feedback.moved"
	FeedbackReplied Kind = "feedback.replied"
	// ChototLinkImported is a check of a Chợ Tốt link that brought new
	// listings over; the payload's count is how many.
	ChototLinkImported Kind = "chotot_link.imported"
	// ChototImportSold is a copy of a Chợ Tốt listing sold here while the
	// listing is still on Chợ Tốt, for a reminder to hide it there.
	ChototImportSold Kind = "chotot_import.sold"
)

// Event is what happened to which aggregate. It is thin: a subscriber reads
// the subject's current state through its module's Service, so order, delay
// and duplicates do not change the result. Payload carries the few fields a
// business event's subscribers need and is usually empty. The field order
// matches the columns Bus selects.
type Event struct {
	ID         uuid.UUID      `json:"id"`
	Kind       Kind           `json:"kind"`
	SubjectID  uuid.UUID      `json:"subject_id"`
	Payload    map[string]any `json:"payload"`
	OccurredAt time.Time      `json:"occurred_at"`
}

// insertEvents writes a batch of events and notifies Bus once; Postgres
// delivers the notification on commit and drops it on rollback.
const insertEvents = `
WITH inserted AS (
    INSERT INTO events (id, kind, subject_id, payload, occurred_at)
    SELECT id, kind, subject_id, payload, occurred_at
    FROM unnest(@ids::uuid[], @kinds::text[], @subjects::uuid[], @payloads::jsonb[], @occurred_at::timestamptz[])
        AS e(id, kind, subject_id, payload, occurred_at)
    ON CONFLICT (id) DO NOTHING)
SELECT pg_notify('events', '')`

// Queue adds to b one statement that writes evs, so they commit with the
// change b carries. Writing an event with the same ID again does nothing,
// so a durable step that retries its save, with the event IDs it took from
// the journal, stays at one event each. No events queues nothing.
func Queue(b *pgx.Batch, evs ...Event) {
	if len(evs) == 0 {
		return
	}
	ids := make([]uuid.UUID, len(evs))
	kinds := make([]string, len(evs))
	subjects := make([]uuid.UUID, len(evs))
	payloads := make([]map[string]any, len(evs))
	occurred := make([]time.Time, len(evs))
	for i, e := range evs {
		ids[i], kinds[i], subjects[i], payloads[i], occurred[i] = e.ID, string(e.Kind), e.SubjectID, e.Payload, e.OccurredAt
		if payloads[i] == nil {
			payloads[i] = map[string]any{}
		}
	}
	b.Queue(insertEvents, pgx.StrictNamedArgs{
		"ids": ids, "kinds": kinds, "subjects": subjects, "payloads": payloads, "occurred_at": occurred,
	}).Fn = postgres.ExecAs("insert events", nil)
}
