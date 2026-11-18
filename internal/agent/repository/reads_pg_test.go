package repository_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

// A read is stored once, and a sweep deletes only the reads before its
// time, in batches.
func TestReadsInsertAndDeleteBefore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	member := uuid.NewV7()
	if _, err := db.Exec(ctx, `
INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, 'Lan', 'moderator', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": member, "email": member.String() + "@console.test", "at": now}); err != nil {
		t.Fatal(err)
	}
	s := repository.NewReads(db)
	old := repository.Read{
		ID: uuid.NewV7(), MemberID: member, Tool: "ticket_get", SubjectKind: domain.SubjectTicket,
		SubjectIDs: []uuid.UUID{uuid.NewV7(), uuid.NewV7()}, At: now.Add(-91 * 24 * time.Hour),
	}
	for _, r := range []repository.Read{
		old, old,
		{ID: uuid.NewV7(), MemberID: member, Tool: "ticket_get", SubjectKind: domain.SubjectTicket, SubjectIDs: old.SubjectIDs, At: now.Add(-92 * 24 * time.Hour)},
		{ID: uuid.NewV7(), MemberID: member, Tool: "tickets_of_account", SubjectKind: domain.SubjectAccount, SubjectIDs: []uuid.UUID{uuid.NewV7()}, At: now},
	} {
		if err := s.Insert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM console_agent_reads`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(); n != 3 {
		t.Fatalf("a repeated insert stored %d rows, want 3", n)
	}
	if n, err := s.DeleteBefore(ctx, now.Add(-90*24*time.Hour), 1); err != nil || n != 1 {
		t.Fatalf("a batch of one: %d %v", n, err)
	}
	if n, err := s.DeleteBefore(ctx, now.Add(-90*24*time.Hour), 10); err != nil || n != 1 {
		t.Fatalf("the rest: %d %v", n, err)
	}
	if n := count(); n != 1 {
		t.Fatalf("%d rows left, want the recent one", n)
	}
}
