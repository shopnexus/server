package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestIssueSessionCreatesThenRotates(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx, sessions := context.Background(), repository.NewSessions(db)
	id, acc := uuid.NewV7(), accounttest.New(t, db)
	first, err := service.IssueSession(at(nil, sessions, now), ctx, id, acc, "Pixel 9")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.NewVerifier(testKeys).Verify(first.AccessToken, now)
	if err != nil || p.SessionID != id || p.AccountID != acc || !p.AuthTime.Equal(now) || first.TokenType != "Bearer" {
		t.Fatalf("%+v %v", p, err)
	}
	again, err := service.IssueSession(at(nil, sessions, now), ctx, id, acc, "Pixel 9")
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
	id, acc := uuid.NewV7(), accounttest.New(t, db)
	if _, err := service.IssueSession(at(nil, sessions, now), ctx, id, acc, ""); err != nil {
		t.Fatal(err)
	}
	s, _ := sessions.Get(ctx, id)
	s.Revoke(domain.RevokedSignOut, now)
	sessions.Save(ctx, &s, uuid.NewV7())
	if _, err := service.IssueSession(at(nil, sessions, now), ctx, id, acc, ""); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("replay after sign-out: %v", err)
	}
}
