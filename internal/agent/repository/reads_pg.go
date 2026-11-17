package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgReads struct{ db *pgxpool.Pool }

// NewReads keeps the reads on db.
func NewReads(db *pgxpool.Pool) Reads { return pgReads{db} }

func (s pgReads) Insert(ctx context.Context, r Read) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO console_agent_reads (id, member_id, tool_name, subject_kind, subject_ids, read_at)
VALUES (@id, @member, @tool, @kind, @ids, @at) ON CONFLICT (id) DO NOTHING`,
		pgx.StrictNamedArgs{
			"id": r.ID, "member": r.MemberID, "tool": r.Tool, "kind": string(r.SubjectKind), "ids": r.SubjectIDs, "at": r.At,
		}); err != nil {
		return fmt.Errorf("insert agent read: %w", err)
	}
	return nil
}

func (s pgReads) DeleteBefore(ctx context.Context, before time.Time, limit int) (int, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM console_agent_reads WHERE id IN (
    SELECT id FROM console_agent_reads WHERE read_at < @before LIMIT @limit)`,
		pgx.StrictNamedArgs{"before": before, "limit": limit})
	if err != nil {
		return 0, fmt.Errorf("delete old agent reads: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
