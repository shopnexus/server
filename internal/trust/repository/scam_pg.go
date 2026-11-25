package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/stream"
	"github.com/shopnexus/server/internal/trust/domain"
)

type pgScamWarnings struct{ db *pgxpool.Pool }

func NewScamWarnings(db *pgxpool.Pool) ScamWarnings { return pgScamWarnings{db} }

const warningColumns = `conversation_id, account_id, suspect_id, signals, dismissed_at, updated_at, write_id`

// pruneBatch caps the checks one Record deletes.
const pruneBatch = 1000

func scanWarning(row pgx.Row) (domain.Warning, error) {
	var st domain.WarningState
	if err := row.Scan(&st.ConversationID, &st.AccountID, &st.SuspectID, &st.Signals, &st.DismissedAt, &st.UpdatedAt,
		&st.WriteID); err != nil {
		return domain.Warning{}, fmt.Errorf("scan scam warning: %w", err)
	}
	return domain.RestoreWarning(st), nil
}

func (s pgScamWarnings) Get(ctx context.Context, conversation, account uuid.UUID) (domain.Warning, error) {
	w, err := scanWarning(s.db.QueryRow(ctx, `SELECT `+warningColumns+` FROM scam_warnings
WHERE conversation_id = @conversation AND account_id = @account`, pgx.StrictNamedArgs{"conversation": conversation, "account": account}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Warning{}, ErrNotFound
	}
	return w, err
}

func (s pgScamWarnings) Of(ctx context.Context, account uuid.UUID, conversations []uuid.UUID) (map[uuid.UUID]domain.Warning, error) {
	rows, err := s.db.Query(ctx, `SELECT `+warningColumns+` FROM scam_warnings
WHERE account_id = @account AND conversation_id = ANY(@conversations)`, pgx.StrictNamedArgs{"account": account, "conversations": conversations})
	if err != nil {
		return nil, fmt.Errorf("select scam warnings: %w", err)
	}
	ws, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Warning, error) { return scanWarning(r) })
	if err != nil {
		return nil, fmt.Errorf("read scam warnings: %w", err)
	}
	out := make(map[uuid.UUID]domain.Warning, len(ws))
	for _, w := range ws {
		out[w.ConversationID()] = w
	}
	return out, nil
}

// queueWarning queues the write of w under writeID when its row has
// w.WriteID() or already writeID, or its add when no row exists, and the
// notice to its account. b must go in a tx: a lost race fails it with
// ErrConflict.
func queueWarning(b *pgx.Batch, w *domain.Warning, writeID uuid.UUID) {
	st := w.State()
	b.Queue(`INSERT INTO scam_warnings (`+warningColumns+`)
VALUES (@conversation, @account, @suspect, @signals::jsonb, @dismissed_at, @updated_at, @write_id)
ON CONFLICT (conversation_id, account_id) DO UPDATE
SET signals = EXCLUDED.signals, dismissed_at = EXCLUDED.dismissed_at, updated_at = EXCLUDED.updated_at,
    write_id = EXCLUDED.write_id
WHERE scam_warnings.write_id = @read_write_id OR scam_warnings.write_id = @write_id`, pgx.StrictNamedArgs{
		"conversation": st.ConversationID, "account": st.AccountID, "suspect": st.SuspectID, "signals": st.Signals,
		"dismissed_at": st.DismissedAt, "updated_at": st.UpdatedAt, "write_id": writeID, "read_write_id": st.WriteID,
	}).Fn = func(br pgx.BatchResults) error {
		tag, err := br.Exec()
		if err != nil {
			return fmt.Errorf("upsert scam warning: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		return nil
	}
	conversation := st.ConversationID
	stream.Queue(b, []uuid.UUID{st.AccountID}, stream.Hint{Kind: stream.KindScamWarning, ConversationID: &conversation})
}

// codeStrings turns codes into the text[] of a check.
func codeStrings(codes []domain.Code) []string {
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = string(c)
	}
	return out
}

func (s pgScamWarnings) Record(ctx context.Context, c domain.Check, ws []WarningWrite, pruneBefore time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		b.Queue(`INSERT INTO scam_checks (id, conversation_id, message_ids, classifier, prompt_version, codes_a,
    codes_b, created_at)
VALUES (@id, @conversation, @messages, @classifier, @prompt_version, @codes_a, @codes_b, @created_at)
ON CONFLICT (id) DO NOTHING`, pgx.StrictNamedArgs{
			"id": c.ID, "conversation": c.ConversationID, "messages": c.MessageIDs,
			"classifier": c.Classifier, "prompt_version": c.PromptVersion, "codes_a": codeStrings(c.A), "codes_b": codeStrings(c.B),
			"created_at": c.CreatedAt,
		}).Fn = execAs("insert scam check")
		// At most the conversation's two sides: one statement each reads
		// clearer than an unnest with a per-row conflict check.
		for _, w := range ws {
			queueWarning(b, w.Warning, w.WriteID)
		}
		b.Queue(`DELETE FROM scam_checks WHERE id IN (
    SELECT id FROM scam_checks WHERE created_at < @before ORDER BY created_at LIMIT @limit)`,
			pgx.StrictNamedArgs{"before": pruneBefore, "limit": pruneBatch}).Fn = execAs("prune scam checks")
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("record scam check: %w", err)
	}
	for _, w := range ws {
		w.Warning.Saved(w.WriteID)
	}
	return nil
}

func (s pgScamWarnings) Dismiss(ctx context.Context, conversation, account uuid.UUID, at time.Time, writeID uuid.UUID) (bool, error) {
	changed := false
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		w, err := scanWarning(tx.QueryRow(ctx, `SELECT `+warningColumns+` FROM scam_warnings
WHERE conversation_id = @conversation AND account_id = @account FOR UPDATE`, pgx.StrictNamedArgs{"conversation": conversation, "account": account}))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !w.Dismiss(at) {
			return nil
		}
		changed = true
		b := &pgx.Batch{}
		queueWarning(b, &w, writeID)
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return false, fmt.Errorf("dismiss scam warning: %w", err)
	}
	return changed, nil
}

func (s pgScamWarnings) Checked(ctx context.Context, conversation uuid.UUID, since time.Time) ([]uuid.UUID, int, error) {
	var (
		last  []uuid.UUID
		count int
	)
	if err := s.db.QueryRow(ctx, `SELECT
    (SELECT message_ids FROM scam_checks WHERE conversation_id = @conversation ORDER BY created_at DESC, id DESC LIMIT 1),
    (SELECT count(*) FROM scam_checks WHERE conversation_id = @conversation AND created_at >= @since)`,
		pgx.StrictNamedArgs{"conversation": conversation, "since": since}).Scan(&last, &count); err != nil {
		return nil, 0, fmt.Errorf("read scam checks: %w", err)
	}
	return last, count, nil
}

func (s pgScamWarnings) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM scam_warnings WHERE account_id = @account`, pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("delete scam warnings: %w", err)
	}
	return nil
}
