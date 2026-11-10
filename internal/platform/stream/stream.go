// Package stream tells open apps that something they show changed. A
// repository calls Notify in the transaction of the change; Hub listens for
// the notifications and hands each hint to the server-sent event streams of
// the accounts it names. A hint carries IDs only, and the app reloads what
// it shows, so nothing is stored or replayed: an app that reconnects
// reloads.
package stream

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const (
	KindMessage = "message"
	KindRead    = "read"
	KindDeal    = "deal"
	// KindNotification is a new or changed notice of the account's.
	KindNotification = "notification"
)

// Hint names what changed: Kind and the IDs that go with it.
type Hint struct {
	Kind           string     `json:"kind"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	MessageID      *uuid.UUID `json:"message_id,omitempty"`
	DealID         *uuid.UUID `json:"deal_id,omitempty"`
	NotificationID *uuid.UUID `json:"notification_id,omitempty"`
}

// Notify sends h to the streams of accounts. Postgres delivers it when tx
// commits and drops it on rollback.
func Notify(ctx context.Context, tx pgx.Tx, accounts []uuid.UUID, h Hint) error {
	targets := make([]Target, 0, len(accounts))
	for _, a := range accounts {
		targets = append(targets, Target{Account: a})
	}
	return PGBell{}.Ring(ctx, tx, targets, h)
}
