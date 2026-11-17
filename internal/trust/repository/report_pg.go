package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/trust/domain"
)

type pgReports struct{ db *pgxpool.Pool }

func NewReports(db *pgxpool.Pool) Reports { return pgReports{db} }

const reportColumns = `id, reporter_id, account_id, listing_id, conversation_id, reason, note, created_at, updated_at,
       withdrawn_at`

// scanReport reads reportColumns, then any more destinations.
func scanReport(row pgx.Row, more ...any) (domain.Report, error) {
	var (
		st                    domain.ReportState
		listing, conversation *uuid.UUID
	)
	dest := append([]any{
		&st.ID, &st.ReporterID, &st.AccountID, &listing, &conversation, &st.Reason, &st.Note, &st.CreatedAt, &st.UpdatedAt,
		&st.WithdrawnAt,
	}, more...)
	if err := row.Scan(dest...); err != nil {
		return domain.Report{}, fmt.Errorf("scan report: %w", err)
	}
	if listing != nil {
		st.ListingID = *listing
	}
	if conversation != nil {
		st.ConversationID = *conversation
	}
	return domain.RestoreReport(st), nil
}

func (s pgReports) Open(ctx context.Context, r domain.Report) (domain.Report, bool, error) {
	st := r.State()
	// Each target has its partial index; ON CONFLICT names the one it hits.
	target := `(reporter_id, account_id) WHERE withdrawn_at IS NULL AND resolution_id IS NULL AND listing_id IS NULL`
	var listing *uuid.UUID
	if st.ListingID != (uuid.UUID{}) {
		target, listing = `(reporter_id, listing_id) WHERE withdrawn_at IS NULL AND resolution_id IS NULL AND listing_id IS NOT NULL`, &st.ListingID
	}
	var conversation *uuid.UUID
	if st.ConversationID != (uuid.UUID{}) {
		conversation = &st.ConversationID
	}
	var added bool
	got, err := scanReport(s.db.QueryRow(ctx, `INSERT INTO reports (`+reportColumns+`)
VALUES (@id, @reporter, @account, @listing, @conversation, @reason, @note, @created_at, @updated_at, NULL)
ON CONFLICT `+target+` DO UPDATE
SET reason = EXCLUDED.reason, note = EXCLUDED.note, conversation_id = EXCLUDED.conversation_id,
    updated_at = EXCLUDED.updated_at
RETURNING `+reportColumns+`, xmax = 0`, pgx.StrictNamedArgs{
		"id": st.ID, "reporter": st.ReporterID, "account": st.AccountID, "listing": listing, "conversation": conversation, "reason": st.Reason,
		"note": st.Note, "created_at": st.CreatedAt, "updated_at": st.UpdatedAt,
	}), &added)
	if err != nil {
		return domain.Report{}, false, fmt.Errorf("upsert report: %w", err)
	}
	return got, added, nil
}

func (s pgReports) Get(ctx context.Context, id uuid.UUID) (domain.Report, error) {
	r, err := scanReport(s.db.QueryRow(ctx, `SELECT `+reportColumns+` FROM reports WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Report{}, ErrNotFound
	}
	return r, err
}

func (s pgReports) Withdraw(ctx context.Context, id uuid.UUID, at time.Time) error {
	if _, err := s.db.Exec(ctx, `UPDATE reports SET withdrawn_at = @at, updated_at = @at
WHERE id = @id AND withdrawn_at IS NULL`, pgx.StrictNamedArgs{"id": id, "at": at}); err != nil {
		return fmt.Errorf("withdraw report: %w", err)
	}
	return nil
}
