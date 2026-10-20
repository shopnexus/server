package auth_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/auth/session/sessiontest"
)

func TestIssueSessionCreatesThenRotates(t *testing.T) {
	ctx, sessions, signer := context.Background(), sessiontest.NewMemStore(), session.NewSigner(testKeys)
	id, acc := uuid.NewV7(), uuid.NewV7()
	first, err := auth.IssueSession(ctx, sessions, signer, id, acc, "Pixel 9", now)
	if err != nil {
		t.Fatal(err)
	}
	p, err := session.NewVerifier(testKeys).Verify(first.AccessToken, now)
	if err != nil || p.SessionID != id || p.AccountID != acc || !p.AuthTime.Equal(now) || first.TokenType != "Bearer" {
		t.Fatalf("%+v %v", p, err)
	}
	again, err := auth.IssueSession(ctx, sessions, signer, id, acc, "Pixel 9", now)
	if err != nil || again.RefreshToken == first.RefreshToken {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if s, err := sessions.FindByRefresh(ctx, session.HashRefresh(again.RefreshToken)); err != nil || s.ID() != id {
		t.Fatalf("the replay's token does not refresh: %v", err)
	}
}
