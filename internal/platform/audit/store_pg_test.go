package audit_test

import (
	"context"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var at = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

// newMember inserts a staff_members row so a staff actor can reference it.
func newMember(t *testing.T, db *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	_, err := db.Exec(context.Background(), `
INSERT INTO staff_members (id, email, display_name, staff_role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, @name, 'moderator', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": id, "email": id.String() + "@staff.test", "name": name, "at": at})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insert(t *testing.T, db *pgxpool.Pool, e audit.Entry) {
	t.Helper()
	if err := pgx.BeginFunc(context.Background(), db, func(tx pgx.Tx) error { return audit.Insert(context.Background(), tx, e) }); err != nil {
		t.Fatal(err)
	}
}

func TestListFiltersAndPages(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := audit.NewPGStore(db)
	ctx := context.Background()
	mod := newMember(t, db, "Mod One")
	acc := uuid.NewV7()
	ip := netip.MustParseAddr("203.0.113.9")

	insert(t, db, audit.Entry{Actor: audit.ByCLI("root"), Action: audit.StaffCreate, Target: audit.Staff(mod), At: at})
	insert(t, db, audit.Entry{
		Actor:  audit.Actor{Kind: audit.ActorStaff, StaffID: mod, IP: ip},
		Action: audit.AccountSuspend, Target: audit.Account(acc), Details: map[string]any{"reason": "spam"}, At: at.Add(time.Second),
	})

	all, err := s.List(ctx, audit.Filter{}, page.Request{Limit: 10})
	if err != nil || len(all) != 2 {
		t.Fatalf("%+v %v", all, err)
	}
	if all[0].Action != audit.AccountSuspend || all[0].Actor.DisplayName != "Mod One" || all[0].IP != "203.0.113.9" ||
		all[0].Target.ID != acc || all[0].Details["reason"] != "spam" {
		t.Fatalf("newest first, with the member's name: %+v", all[0])
	}
	if all[1].Actor.Kind != audit.ActorCLI || all[1].Actor.DisplayName != "root" || all[1].IP != "" ||
		all[1].Actor.StaffID != nil || len(all[1].Details) != 0 {
		t.Fatalf("cli row: %+v", all[1])
	}

	byTarget, _ := s.List(ctx, audit.Filter{TargetID: acc}, page.Request{Limit: 10})
	byStaff, _ := s.List(ctx, audit.Filter{StaffID: mod}, page.Request{Limit: 10})
	if len(byTarget) != 1 || len(byStaff) != 1 {
		t.Fatalf("filters: %d %d", len(byTarget), len(byStaff))
	}

	first, _ := s.List(ctx, audit.Filter{}, page.Request{Limit: 1})
	rest, _ := s.List(ctx, audit.Filter{}, page.Request{Cursor: first[0].ID, Limit: 5})
	if len(first) != 2 || len(rest) != 1 || rest[0].ID == first[0].ID {
		t.Fatalf("pages: %d then %d", len(first), len(rest))
	}
}
