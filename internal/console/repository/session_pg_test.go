package repository_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/repository"
	"github.com/shopnexus/server/internal/platform/token"
)

func TestLiveFollowsTheMember(t *testing.T) {
	t.Parallel()
	members, db := newStore(t)
	ctx := context.Background()
	v := newMember(t, domain.RoleModerator)
	created(t, members, v)
	tok, hash := token.NewOpaque("ss_")
	s := domain.Session{
		ID: uuid.NewV7(), MemberID: v.ID(), TokenHash: hash, CredentialsVersion: 1,
		IP: clientIP, UserAgent: "Firefox", CreatedAt: now, ExpiresAt: now.Add(domain.SessionTTL),
	}
	st := repository.NewSessions(db)
	if err := st.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	g, err := st.Live(ctx, token.Hash(tok), now)
	if err != nil || g.SessionID != s.ID || g.MemberID != v.ID() || g.Role != domain.RoleModerator {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := st.Live(ctx, token.Hash(tok), s.ExpiresAt); err == nil {
		t.Fatal("live at its end")
	}
	if _, err := db.Exec(ctx, `UPDATE console_members SET credentials_version = 2 WHERE id = @id`, pgx.StrictNamedArgs{"id": v.ID()}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Live(ctx, token.Hash(tok), now); err == nil {
		t.Fatal("live after a credentials change")
	}
	if _, err := db.Exec(ctx, `UPDATE console_members SET credentials_version = 1 WHERE id = @id`, pgx.StrictNamedArgs{"id": v.ID()}); err != nil {
		t.Fatal(err)
	}
	if err := st.Revoke(ctx, s.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Live(ctx, token.Hash(tok), now); err == nil {
		t.Fatal("live after a revoke")
	}
}
