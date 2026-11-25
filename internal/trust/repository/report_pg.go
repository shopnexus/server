package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/trust/domain"
)

type pgReports struct{ db *pgxpool.Pool }

func NewReports(db *pgxpool.Pool) Reports { return pgReports{db} }

const reportColumns = `id, reporter_id, account_id, listing_id, conversation_id, reason, note, created_at, updated_at,
       withdrawn_at`

// reportSelect is reportColumns and whether a case resolved the report.
const reportSelect = reportColumns + `, resolution_id IS NOT NULL`

// scanReport reads reportSelect, then any more destinations.
func scanReport(row pgx.Row, more ...any) (domain.Report, error) {
	var (
		st                    domain.ReportState
		listing, conversation *uuid.UUID
	)
	dest := append([]any{
		&st.ID, &st.ReporterID, &st.AccountID, &listing, &conversation, &st.Reason, &st.Note, &st.CreatedAt, &st.UpdatedAt,
		&st.WithdrawnAt, &st.Resolved,
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
	var (
		got   domain.Report
		added bool
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		// A report that commits after Erase waits for it here and finds the
		// tombstone, so none outlives the worker's WithdrawAll.
		postgres.QueueLockLiving(b, st.ReporterID, accountdomain.ErrSessionRevoked)
		b.Queue(`INSERT INTO reports (`+reportColumns+`)
VALUES (@id, @reporter, @account, @listing, @conversation, @reason, @note, @created_at, @updated_at, NULL)
ON CONFLICT `+target+` DO UPDATE
SET reason = EXCLUDED.reason, note = EXCLUDED.note, conversation_id = EXCLUDED.conversation_id,
    updated_at = EXCLUDED.updated_at
RETURNING `+reportSelect+`, xmax = 0`, pgx.StrictNamedArgs{
			"id": st.ID, "reporter": st.ReporterID, "account": st.AccountID, "listing": listing, "conversation": conversation, "reason": st.Reason,
			"note": st.Note, "created_at": st.CreatedAt, "updated_at": st.UpdatedAt,
		}).QueryRow(func(row pgx.Row) error {
			var scanErr error
			got, scanErr = scanReport(row, &added)
			return scanErr
		})
		return tx.SendBatch(ctx, b).Close()
	})
	if errors.Is(err, accountdomain.ErrSessionRevoked) {
		return domain.Report{}, false, accountdomain.ErrSessionRevoked
	}
	if err != nil {
		return domain.Report{}, false, fmt.Errorf("upsert report: %w", err)
	}
	return got, added, nil
}

func (s pgReports) Get(ctx context.Context, id uuid.UUID) (domain.Report, error) {
	r, err := scanReport(s.db.QueryRow(ctx, `SELECT `+reportSelect+` FROM reports WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Report{}, ErrNotFound
	}
	return r, err
}

func (s pgReports) ListMine(ctx context.Context, reporter uuid.UUID, p page.Request) ([]domain.Report, error) {
	rows, err := s.db.Query(ctx, `SELECT `+reportSelect+` FROM reports
WHERE reporter_id = @reporter AND id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"reporter": reporter, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select reports: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Report, error) { return scanReport(row) })
	if err != nil {
		return nil, fmt.Errorf("read reports: %w", err)
	}
	return out, nil
}

func (s pgReports) Withdraw(ctx context.Context, id uuid.UUID, at time.Time) error {
	if _, err := s.db.Exec(ctx, `UPDATE reports SET withdrawn_at = @at, updated_at = @at
WHERE id = @id AND withdrawn_at IS NULL`, pgx.StrictNamedArgs{"id": id, "at": at}); err != nil {
		return fmt.Errorf("withdraw report: %w", err)
	}
	return nil
}

func (s pgReports) WithdrawAll(ctx context.Context, reporter uuid.UUID, at time.Time) error {
	if _, err := s.db.Exec(ctx, `UPDATE reports SET withdrawn_at = coalesce(withdrawn_at, @at), note = '', updated_at = @at
WHERE reporter_id = @reporter AND resolution_id IS NULL AND (withdrawn_at IS NULL OR note <> '')`,
		pgx.StrictNamedArgs{"reporter": reporter, "at": at}); err != nil {
		return fmt.Errorf("withdraw the reporter's reports: %w", err)
	}
	return nil
}

func (s pgReports) CountOpen(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM reports WHERE withdrawn_at IS NULL AND resolution_id IS NULL`).
		Scan(&n); err != nil {
		return 0, fmt.Errorf("count open reports: %w", err)
	}
	return n, nil
}
