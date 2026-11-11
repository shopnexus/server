package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/stream"
	"github.com/shopnexus/server2/internal/trust/domain"
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

// putWarning writes w under writeID when its row has w.WriteID(), or adds
// it when it has none and no row exists, then notifies its account.
func putWarning(ctx context.Context, tx pgx.Tx, w *domain.Warning, writeID uuid.UUID) error {
	st := w.State()
	tag, err := tx.Exec(ctx, `INSERT INTO scam_warnings (`+warningColumns+`)
VALUES (@conversation, @account, @suspect, @signals::jsonb, @dismissed_at, @updated_at, @write_id)
ON CONFLICT (conversation_id, account_id) DO UPDATE
SET signals = EXCLUDED.signals, dismissed_at = EXCLUDED.dismissed_at, updated_at = EXCLUDED.updated_at,
    write_id = EXCLUDED.write_id
WHERE scam_warnings.write_id = @old`, pgx.StrictNamedArgs{
		"conversation": st.ConversationID, "account": st.AccountID, "suspect": st.SuspectID, "signals": st.Signals,
		"dismissed_at": st.DismissedAt, "updated_at": st.UpdatedAt, "write_id": writeID, "old": st.WriteID,
	})
	if err != nil {
		return fmt.Errorf("upsert scam warning: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	conversation := st.ConversationID
	if err = stream.Notify(ctx, tx, []uuid.UUID{st.AccountID},
		stream.Hint{Kind: stream.KindScamWarning, ConversationID: &conversation}); err != nil {
		return fmt.Errorf("notify the scam warning: %w", err)
	}
	return nil
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
		if _, err := tx.Exec(ctx, `INSERT INTO scam_checks (id, conversation_id, first_message_id, last_message_id, classifier,
    prompt_version, codes_a, codes_b, created_at)
VALUES (@id, @conversation, @first, @last, @classifier, @prompt_version, @codes_a, @codes_b, @created_at)
ON CONFLICT (id) DO NOTHING`, pgx.StrictNamedArgs{
			"id": c.ID, "conversation": c.ConversationID, "first": c.FirstMessageID, "last": c.LastMessageID,
			"classifier": c.Classifier, "prompt_version": c.PromptVersion, "codes_a": codeStrings(c.A), "codes_b": codeStrings(c.B),
			"created_at": c.CreatedAt,
		}); err != nil {
			return fmt.Errorf("insert scam check: %w", err)
		}
		for _, w := range ws {
			if err := putWarning(ctx, tx, w.Warning, w.WriteID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM scam_checks WHERE id IN (
    SELECT id FROM scam_checks WHERE created_at < @before ORDER BY created_at LIMIT @limit)`,
			pgx.StrictNamedArgs{"before": pruneBefore, "limit": pruneBatch}); err != nil {
			return fmt.Errorf("prune scam checks: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record scam check: %w", err)
	}
	for _, w := range ws {
		w.Warning.Saved(w.WriteID)
	}
	return nil
}

func (s pgScamWarnings) Save(ctx context.Context, w *domain.Warning, writeID uuid.UUID) error {
	if err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error { return putWarning(ctx, tx, w, writeID) }); err != nil {
		return fmt.Errorf("save scam warning: %w", err)
	}
	w.Saved(writeID)
	return nil
}

func (s pgScamWarnings) Checked(ctx context.Context, conversation uuid.UUID, since time.Time) (uuid.UUID, int, error) {
	var (
		last  *uuid.UUID
		count int
	)
	if err := s.db.QueryRow(ctx, `SELECT
    (SELECT last_message_id FROM scam_checks WHERE conversation_id = @conversation ORDER BY created_at DESC, id DESC LIMIT 1),
    (SELECT count(*) FROM scam_checks WHERE conversation_id = @conversation AND created_at >= @since)`,
		pgx.StrictNamedArgs{"conversation": conversation, "since": since}).Scan(&last, &count); err != nil {
		return uuid.UUID{}, 0, fmt.Errorf("read scam checks: %w", err)
	}
	if last == nil {
		return uuid.UUID{}, count, nil
	}
	return *last, count, nil
}

func (s pgScamWarnings) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM scam_warnings WHERE account_id = @account`, pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("delete scam warnings: %w", err)
	}
	return nil
}
