package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func TestIssueSessionCreatesThenRotates(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx, sessions := context.Background(), repository.NewSessions(db)
	id, acc := uuid.NewV7(), newAccount(t, db)
	first, err := authWith(sessions, nil, now, discard).IssueSession(ctx, id, acc, "Pixel 9")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.NewVerifier(testKeys).Verify(first.AccessToken, now)
	if err != nil || p.SessionID != id || p.AccountID != acc || !p.AuthTime.Equal(now) || first.TokenType != "Bearer" {
		t.Fatalf("%+v %v", p, err)
	}
	again, err := authWith(sessions, nil, now, discard).IssueSession(ctx, id, acc, "Pixel 9")
	if err != nil || again.RefreshToken == first.RefreshToken {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if s, err := sessions.FindByRefresh(ctx, domain.HashRefresh(again.RefreshToken)); err != nil || s.ID() != id {
		t.Fatalf("the replay's token does not refresh: %v", err)
	}
}

func TestIssueSessionRefusesASignedOutSession(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx, sessions := context.Background(), repository.NewSessions(db)
	id, acc := uuid.NewV7(), newAccount(t, db)
	if _, err := authWith(sessions, nil, now, discard).IssueSession(ctx, id, acc, ""); err != nil {
		t.Fatal(err)
	}
	s, _ := sessions.Get(ctx, id)
	s.Revoke(now)
	sessions.Save(ctx, &s, uuid.NewV7())
	if _, err := authWith(sessions, nil, now, discard).IssueSession(ctx, id, acc, ""); !errors.Is(err, domain.ErrRevoked) {
		t.Fatalf("replay after sign-out: %v", err)
	}
}
