// Package stream tells open apps that something they show changed. A
// repository calls Notify in the transaction of the change; Hub listens for
// the notifications and hands each hint to the server-sent event streams of
// the accounts it names. A hint carries IDs only, and the app reloads what
// it shows, so nothing is stored or replayed: an app that reconnects
// reloads.
package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const (
	KindMessage = "message"
	KindRead    = "read"
	KindDeal    = "deal"
)

// channel is the Postgres channel the hints travel on.
const channel = "stream"

// Hint names what changed: Kind is the event name, the IDs its data.
type Hint struct {
	Kind           string     `json:"-"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	MessageID      *uuid.UUID `json:"message_id,omitempty"`
	DealID         *uuid.UUID `json:"deal_id,omitempty"`
}

// notification is a hint and its accounts, as they travel on the channel.
type notification struct {
	Accounts []uuid.UUID `json:"accounts"`
	Kind     string      `json:"kind"`
	Hint     Hint        `json:"hint"`
}

// Notify sends h to the streams of accounts. Postgres delivers it when tx
// commits and drops it on rollback.
func Notify(ctx context.Context, tx pgx.Tx, accounts []uuid.UUID, h Hint) error {
	payload, err := json.Marshal(notification{Accounts: accounts, Kind: h.Kind, Hint: h})
	if err != nil {
		return fmt.Errorf("encode hint: %w", err)
	}
	if _, err = tx.Exec(ctx, `SELECT pg_notify(@channel, @payload)`,
		pgx.StrictNamedArgs{"channel": channel, "payload": string(payload)}); err != nil {
		return fmt.Errorf("notify stream: %w", err)
	}
	return nil
}
