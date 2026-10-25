package repository_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/repository"
)

func TestLiveFollowsTheMember(t *testing.T) {
	t.Parallel()
	members, db := newStore(t)
	ctx := context.Background()
	v := newMember(t, domain.RoleModerator)
	created(t, members, v)
	tok, hash := token.NewOpaque("ss_")
	s := domain.Session{
		ID: uuid.NewV7(), StaffID: v.ID(), TokenHash: hash, CredentialsVersion: 1,
		IP: clientIP, UserAgent: "Firefox", CreatedAt: now, ExpiresAt: now.Add(domain.SessionTTL),
	}
	st := repository.NewSessions(db)
	if err := st.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	g, err := st.Live(ctx, token.Hash(tok), now)
	if err != nil || g.SessionID != s.ID || g.StaffID != v.ID() || g.Role != domain.RoleModerator {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := st.Live(ctx, token.Hash(tok), s.ExpiresAt); err == nil {
		t.Fatal("live at its end")
	}
	if _, err := db.Exec(ctx, `UPDATE staff_members SET credentials_version = 2 WHERE id = @id`, pgx.StrictNamedArgs{"id": v.ID()}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Live(ctx, token.Hash(tok), now); err == nil {
		t.Fatal("live after a credentials change")
	}
	if _, err := db.Exec(ctx, `UPDATE staff_members SET credentials_version = 1 WHERE id = @id`, pgx.StrictNamedArgs{"id": v.ID()}); err != nil {
		t.Fatal(err)
	}
	if err := st.Revoke(ctx, s.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Live(ctx, token.Hash(tok), now); err == nil {
		t.Fatal("live after a revoke")
	}
}
