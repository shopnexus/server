package auth_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

func TestIssueSessionCreatesThenRotates(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx, sessions, signer := context.Background(), session.NewPGStore(db), session.NewSigner(testKeys)
	id, acc := uuid.NewV7(), newAccount(t, db)
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

func TestIssueSessionRefusesASignedOutSession(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx, sessions, signer := context.Background(), session.NewPGStore(db), session.NewSigner(testKeys)
	id, acc := uuid.NewV7(), newAccount(t, db)
	if _, err := auth.IssueSession(ctx, sessions, signer, id, acc, "", now); err != nil {
		t.Fatal(err)
	}
	s, _ := sessions.Get(ctx, id)
	s.Revoke(now)
	sessions.Save(ctx, &s, uuid.NewV7())
	if _, err := auth.IssueSession(ctx, sessions, signer, id, acc, "", now); !errors.Is(err, session.ErrRevoked) {
		t.Fatalf("replay after sign-out: %v", err)
	}
}
