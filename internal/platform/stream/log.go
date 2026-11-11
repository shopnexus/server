package stream

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// keepUpdates is how long the log keeps an update; an app further
	// behind gets reset.
	keepUpdates = 30 * 24 * time.Hour
	pruneBatch  = 10_000
)

// Page is a stretch of an account's log: the UpdatePage schema in
// api/schemas/stream.yaml.
type Page struct {
	Updates []Update `json:"updates"`
	// Seq is the account's current seq.
	Seq int64 `json:"seq"`
	// Reset means the log no longer holds the update after since, or since
	// is ahead of the log: the app reloads everything and keeps Seq.
	Reset   bool `json:"reset"`
	HasMore bool `json:"has_more"`
}

// UpdateLog reads and prunes the accounts' update logs.
type UpdateLog interface {
	// Current returns account's latest seq, 0 before its first update.
	Current(ctx context.Context, account uuid.UUID) (int64, error)
	// Since returns up to limit of account's updates after since, in seq
	// order.
	Since(ctx context.Context, account uuid.UUID, since int64, limit int) (Page, error)
	// Prune deletes the updates written before before, 10,000 at a time,
	// and returns how many it deleted.
	Prune(ctx context.Context, before time.Time) (int64, error)
}

// PGLog is the update log in Postgres.
type PGLog struct {
	DB *pgxpool.Pool
}

var _ UpdateLog = PGLog{}

func (l PGLog) Current(ctx context.Context, account uuid.UUID) (int64, error) {
	var seq int64
	if err := l.DB.QueryRow(ctx, `SELECT coalesce(max(seq), 0) FROM account_update_seqs WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read the current seq: %w", err)
	}
	return seq, nil
}

// Since reads the current seq first and no update past it: every seq up to
// the current one has committed, so a missing since+1 means it was pruned.
func (l PGLog) Since(ctx context.Context, account uuid.UUID, since int64, limit int) (Page, error) {
	current, err := l.Current(ctx, account)
	if err != nil {
		return Page{}, err
	}
	if since > current {
		return Page{Updates: []Update{}, Seq: current, Reset: true}, nil
	}
	if since == current {
		return Page{Updates: []Update{}, Seq: current}, nil
	}
	rows, err := l.DB.Query(ctx, `
SELECT seq, kind, conversation_id, message_id, deal_id, notification_id, video_read_id, link_read_id, listing_read_id FROM account_updates
WHERE account_id = @account AND seq > @since AND seq <= @current
ORDER BY seq LIMIT @limit`,
		pgx.StrictNamedArgs{"account": account, "since": since, "current": current, "limit": limit + 1})
	if err != nil {
		return Page{}, fmt.Errorf("read updates: %w", err)
	}
	updates, err := pgx.AppendRows(make([]Update, 0, limit+1), rows, func(row pgx.CollectableRow) (Update, error) {
		var u Update
		if scanErr := row.Scan(&u.Seq, &u.Kind, &u.ConversationID, &u.MessageID, &u.DealID, &u.NotificationID,
			&u.VideoReadID, &u.LinkReadID, &u.ListingReadID); scanErr != nil {
			return Update{}, fmt.Errorf("scan update: %w", scanErr)
		}
		return u, nil
	})
	if err != nil {
		return Page{}, fmt.Errorf("read updates: %w", err)
	}
	if len(updates) == 0 || updates[0].Seq != since+1 {
		return Page{Updates: []Update{}, Seq: current, Reset: true}, nil
	}
	more := len(updates) > limit
	if more {
		updates = updates[:limit]
	}
	return Page{Updates: updates, Seq: current, HasMore: more}, nil
}

func (l PGLog) Prune(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	for {
		tag, err := l.DB.Exec(ctx, `
DELETE FROM account_updates WHERE (account_id, seq) IN (
    SELECT account_id, seq FROM account_updates WHERE created_at < @before LIMIT @limit)`,
			pgx.StrictNamedArgs{"before": before, "limit": pruneBatch})
		if err != nil {
			return total, fmt.Errorf("prune updates: %w", err)
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < pruneBatch {
			return total, nil
		}
	}
}
