// Package events is server2's outbox. A repository writes an Event with
// Insert in the transaction of the change it announces, and Bus hands it to
// the Restate handlers subscribed to its kind.
package events

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

// Kind is what happened, in the form aggregate.past_tense.
type Kind string

const (
	AccountRenamed     Kind = "account.renamed"
	AccountSuspended   Kind = "account.suspended"
	AccountUnsuspended Kind = "account.unsuspended"
	ListingPublished   Kind = "listing.published"
	ListingUpdated     Kind = "listing.updated"
	ListingHidden      Kind = "listing.hidden"
	ListingUnhidden    Kind = "listing.unhidden"
	ListingSold        Kind = "listing.sold"
	ListingUnsold      Kind = "listing.unsold"
	ListingDeleted     Kind = "listing.deleted"
	ListingReserved    Kind = "listing.reserved"
	ListingReleased    Kind = "listing.released"
	DealMoved          Kind = "deal.moved"
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

const insertEvent = `
INSERT INTO events (id, kind, subject_id, payload, occurred_at)
VALUES (@id, @kind, @subject_id, @payload, @occurred_at)
ON CONFLICT (id) DO NOTHING`

// Insert writes e in tx, the transaction of the change, and notifies Bus;
// Postgres delivers the notification on commit and drops it on rollback.
// Writing an event with the same ID again does nothing, so a durable step
// that retries its save, with the event ID it took from the journal, stays
// at one event.
func Insert(ctx context.Context, tx pgx.Tx, e Event) error {
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	if _, err := tx.Exec(ctx, insertEvent, pgx.StrictNamedArgs{
		"id": e.ID, "kind": e.Kind, "subject_id": e.SubjectID, "payload": payload, "occurred_at": e.OccurredAt,
	}); err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_notify('events', '')`); err != nil {
		return fmt.Errorf("notify events: %w", err)
	}
	return nil
}
