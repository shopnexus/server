package staff_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
)

func TestLiveFollowsTheMember(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	v, _ := e.create(t, "mod@example.com", member.RoleModerator)
	tok, hash := token.NewOpaque("ss_")
	s := staff.Session{
		ID: uuid.NewV7(), StaffID: v.ID, TokenHash: hash, CredentialsVersion: 1,
		IP: clientIP, UserAgent: "Firefox", CreatedAt: now, ExpiresAt: now.Add(staff.SessionTTL),
	}
	st := e.stores.Sessions
	if err := st.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	g, err := st.Live(ctx, token.Hash(tok), now)
	if err != nil || g.SessionID != s.ID || g.StaffID != v.ID || g.Role != member.RoleModerator {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := st.Live(ctx, token.Hash(tok), s.ExpiresAt); err == nil {
		t.Fatal("live at its end")
	}
	if _, err := e.db.Exec(ctx, `UPDATE staff_members SET credentials_version = 2 WHERE id = @id`, pgx.StrictNamedArgs{"id": v.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Live(ctx, token.Hash(tok), now); err == nil {
		t.Fatal("live after a credentials change")
	}
	if _, err := e.db.Exec(ctx, `UPDATE staff_members SET credentials_version = 1 WHERE id = @id`, pgx.StrictNamedArgs{"id": v.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.Revoke(ctx, s.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Live(ctx, token.Hash(tok), now); err == nil {
		t.Fatal("live after a revoke")
	}
}
