// Package stream tells open apps what changed for their account. A
// repository calls Queue with the batch of the change: it appends the change
// to each account's update log, where it takes the next seq, and rings the
// bell. The hub hands rings to the WebSocket connections of their
// accounts, and a connection that may have missed one reads the log. An
// update carries IDs only; the app reloads what it shows.
package stream

import (
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/postgres"
)

const (
	KindMessage = "message"
	KindRead    = "read"
	KindTicket  = "ticket"
	KindDeal    = "deal"
	// KindNotification is a new or changed notice of the account's.
	KindNotification = "notification"
	// KindListingRead is a step of a read of the account's (a video, a
	// photo or a link) finishing.
	KindListingRead = "listing_read"
	// KindScamWarning is a scam warning of the account's raised, grown or
	// dismissed, with its conversation.
	KindScamWarning = "scam_warning"
	// KindTyping is the other member typing in a conversation. It is rung
	// with no update in the log.
	KindTyping = "typing"
)

// loggedKinds are the kinds an update in the log may have; typing is rung
// only.
var loggedKinds = []string{KindMessage, KindRead, KindDeal, KindNotification, KindListingRead, KindScamWarning, KindTicket}

// Hint names what changed: Kind and the IDs that go with it.
type Hint struct {
	Kind           string     `json:"kind"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	MessageID      *uuid.UUID `json:"message_id,omitempty"`
	DealID         *uuid.UUID `json:"deal_id,omitempty"`
	NotificationID *uuid.UUID `json:"notification_id,omitempty"`
	ListingReadID  *uuid.UUID `json:"listing_read_id,omitempty"`
}

// Update is a hint in an account's log, at its seq.
type Update struct {
	Hint

	Seq int64 `json:"seq"`
}

// appendUpdates takes the next seq of each account, in UUID order so two
// transactions cannot deadlock, writes its update and rings the bell. The
// counter rows stay locked until the transaction ends.
const appendUpdates = `
WITH next AS (
    INSERT INTO account_update_seqs AS s (account_id, seq)
    SELECT DISTINCT a, 1 FROM unnest(@accounts::uuid[]) a ORDER BY a
    ON CONFLICT (account_id) DO UPDATE SET seq = s.seq + 1
    RETURNING account_id, seq),
logged AS (
    INSERT INTO account_updates (account_id, seq, kind, conversation_id, message_id, deal_id, notification_id, listing_read_id)
    SELECT account_id, seq, @kind::text, @conversation::uuid, @message::uuid, @deal::uuid, @notification::uuid, @listing_read::uuid
    FROM next
    RETURNING account_id, seq)
SELECT pg_notify(@channel, json_build_object(
    'targets', json_agg(json_build_object('account', account_id, 'seq', seq)), 'hint', @hint::json)::text)
FROM logged`

// Queue adds to b one statement that appends h to the log of each of
// accounts and rings the bell: Postgres rings on commit and drops it all
// on rollback. No accounts queues nothing.
func Queue(b *pgx.Batch, accounts []uuid.UUID, h Hint) {
	if len(accounts) == 0 {
		return
	}
	b.Queue(appendUpdates, pgx.StrictNamedArgs{
		"accounts": accounts, "kind": h.Kind, "conversation": h.ConversationID, "message": h.MessageID,
		"deal": h.DealID, "notification": h.NotificationID, "listing_read": h.ListingReadID,
		"channel": channel, "hint": h,
	}).Fn = postgres.ExecAs("append updates", nil)
}
