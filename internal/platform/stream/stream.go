// Package stream tells open apps what changed for their account. A
// repository calls Notify in the transaction of the change: Notify appends
// the change to each account's update log, where it takes the next seq, and
// rings the bell. The hub hands rings to the WebSocket connections of their
// accounts, and a connection that may have missed one reads the log. An
// update carries IDs only; the app reloads what it shows.
package stream

import (
	"context"
	"fmt"
	"slices"
	"uuid"

	"github.com/jackc/pgx/v5"
)

const (
	KindMessage = "message"
	KindRead    = "read"
	KindDeal    = "deal"
	// KindNotification is a new or changed notice of the account's.
	KindNotification = "notification"
	// KindTyping is the other member typing in a conversation. It is rung
	// with no update in the log.
	KindTyping = "typing"
)

// Hint names what changed: Kind and the IDs that go with it.
type Hint struct {
	Kind           string     `json:"kind"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	MessageID      *uuid.UUID `json:"message_id,omitempty"`
	DealID         *uuid.UUID `json:"deal_id,omitempty"`
	NotificationID *uuid.UUID `json:"notification_id,omitempty"`
}

// Update is a hint in an account's log, at its seq.
type Update struct {
	Hint

	Seq int64 `json:"seq"`
}

// appendUpdate takes the account's next seq and writes its update. The
// counter row stays locked until the transaction ends.
const appendUpdate = `
WITH next AS (
    INSERT INTO account_update_seqs (account_id, seq) VALUES (@account, 1)
    ON CONFLICT (account_id) DO UPDATE SET seq = account_update_seqs.seq + 1
    RETURNING seq)
INSERT INTO account_updates (account_id, seq, kind, conversation_id, message_id, deal_id, notification_id)
SELECT @account, seq, @kind, @conversation, @message, @deal, @notification FROM next
RETURNING seq`

// Notify appends h to the log of each of accounts and rings the bell, in
// tx: Postgres rings on commit and drops it all on rollback. It takes the
// accounts' counters in UUID order, so two transactions that notify the
// same accounts cannot deadlock.
func Notify(ctx context.Context, tx pgx.Tx, accounts []uuid.UUID, h Hint) error {
	ids := slices.Clone(accounts)
	slices.SortFunc(ids, uuid.UUID.Compare)
	ids = slices.Compact(ids)
	targets := make([]Target, 0, len(ids))
	for _, a := range ids {
		var seq int64
		if err := tx.QueryRow(ctx, appendUpdate, pgx.StrictNamedArgs{
			"account": a, "kind": h.Kind, "conversation": h.ConversationID, "message": h.MessageID,
			"deal": h.DealID, "notification": h.NotificationID,
		}).Scan(&seq); err != nil {
			return fmt.Errorf("append update: %w", err)
		}
		targets = append(targets, Target{Account: a, Seq: seq})
	}
	return PGBell{}.Ring(ctx, tx, targets, h)
}
