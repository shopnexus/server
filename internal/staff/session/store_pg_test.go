package session_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/session"
)

// newMember inserts an active moderator at credentials version 1.
func newMember(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	_, err := db.Exec(context.Background(), `
INSERT INTO staff_members (id, email, display_name, role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, 'Mod', 'moderator', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": id, "email": id.String() + "@staff.test", "at": now})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func login(staffID, sessionID uuid.UUID) audit.Entry {
	return audit.Entry{
		Actor:  audit.Actor{Kind: audit.ActorStaff, StaffID: staffID, SessionID: sessionID, Name: "", IP: clientIP},
		Action: "staff.login", Target: audit.Staff(staffID), Diff: nil, At: now,
	}
}

func TestStore(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	st := session.NewPGStore(db)
	ctx := context.Background()
	staffID := newMember(t, db)
	s, refresh := session.New(uuid.NewV7(), staffID, 1, clientIP, "Firefox", now)
	if err := st.CreateAudited(ctx, s, login(staffID, s.ID())); err != nil {
		t.Fatal(err)
	}
	got, err := st.FindByRefresh(ctx, token.HashRefresh(refresh))
	if err != nil || got.ID() != s.ID() || got.IP() != clientIP || got.UserAgent() != "Firefox" {
		t.Fatalf("%+v %v", got, err)
	}
	stale := got
	next := got.Rotate(now.Add(time.Minute))
	if err := st.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindByRefresh(ctx, token.HashRefresh(next)); err != nil {
		t.Fatalf("by the new token: %v", err)
	}
	if prev, err := st.FindByPrevRefresh(ctx, token.HashRefresh(refresh)); err != nil || prev.ID() != s.ID() {
		t.Fatalf("by the old token: %v", err)
	}
	stale.Revoke(now)
	if err := st.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, session.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if _, err := st.FindByRefresh(ctx, []byte("nothing")); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("unknown token: %v", err)
	}
}

func TestGrantFollowsTheMember(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	st := session.NewPGStore(db)
	ctx := context.Background()
	staffID := newMember(t, db)
	s, _ := session.New(uuid.NewV7(), staffID, 1, clientIP, "", now)
	if err := st.CreateAudited(ctx, s, login(staffID, s.ID())); err != nil {
		t.Fatal(err)
	}
	g, err := st.Grant(ctx, s.ID(), staffID)
	if err != nil || g.Role != member.RoleModerator || !g.Active || !g.Current || g.Check(now) != nil {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := db.Exec(ctx, `UPDATE staff_members SET credentials_version = 2 WHERE id = @id`, pgx.StrictNamedArgs{"id": staffID}); err != nil {
		t.Fatal(err)
	}
	if g, _ = st.Grant(ctx, s.ID(), staffID); g.Current || !errors.Is(g.Check(now), session.ErrRevoked) {
		t.Fatalf("after a credentials change: %+v", g)
	}
	if _, err := st.Grant(ctx, s.ID(), uuid.NewV7()); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("another member's session: %v", err)
	}
}

func TestMoveVersion(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	st := session.NewPGStore(db)
	ctx := context.Background()
	staffID := newMember(t, db)
	s, _ := session.New(uuid.NewV7(), staffID, 1, clientIP, "", now)
	if err := st.CreateAudited(ctx, s, login(staffID, s.ID())); err != nil {
		t.Fatal(err)
	}
	if err := st.MoveVersion(ctx, s.ID(), 2, 3); !errors.Is(err, session.ErrRevoked) {
		t.Fatalf("from a version it is not at: %v", err)
	}
	if err := st.MoveVersion(ctx, s.ID(), 1, 2); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(ctx, s.ID())
	if got.CredentialsVersion() != 2 {
		t.Fatalf("version %d", got.CredentialsVersion())
	}
}
