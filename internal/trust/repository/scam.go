package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/trust/domain"
)

// ScamWarnings persists scam warnings, one per member of a conversation,
// and the checks that raise them.
type ScamWarnings interface {
	// Get returns account's warning in conversation, or ErrNotFound.
	Get(ctx context.Context, conversation, account uuid.UUID) (domain.Warning, error)
	// Of returns account's warnings in the conversations given, keyed by
	// conversation; a conversation without one is left out.
	Of(ctx context.Context, account uuid.UUID, conversations []uuid.UUID) (map[uuid.UUID]domain.Warning, error)
	// Record writes check c once by its ID, each warning of ws under its
	// WriteID with an update to the warning's account, and deletes up to
	// 1,000 checks created before pruneBefore, in one transaction. A warning
	// whose row has another write ID than its own, or a new one whose row
	// exists, is ErrConflict and nothing is written.
	Record(ctx context.Context, c domain.Check, ws []WarningWrite, pruneBefore time.Time) error
	// Dismiss dismisses account's warning in conversation at at, under
	// writeID, with an update to account. It holds the row's lock from the
	// read to the write, so it never conflicts; it reports false, writing
	// nothing, without a warning or when the warning is dismissed already.
	Dismiss(ctx context.Context, conversation, account uuid.UUID, at time.Time, writeID uuid.UUID) (bool, error)
	// Checked returns the messages of conversation's newest check, nil
	// without one, and how many checks it had since since.
	Checked(ctx context.Context, conversation uuid.UUID, since time.Time) (last []uuid.UUID, count int, err error)
	// DeleteAccount deletes account's own warnings; the warnings about it
	// stay.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
}

// WarningWrite is a warning to write and the write ID it gets.
type WarningWrite struct {
	Warning *domain.Warning
	WriteID uuid.UUID
}
