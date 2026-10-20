package auth_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/auth/session/sessiontest"
)

type refreshEnv struct {
	sessions *sessiontest.MemStore
	accounts *fakeAccounts
	acc      uuid.UUID
	sid      uuid.UUID
	token    string
}

func newRefreshEnv(t *testing.T) *refreshEnv {
	t.Helper()
	e := &refreshEnv{sessions: sessiontest.NewMemStore(), accounts: newFakeAccounts(), acc: uuid.NewV7(), sid: uuid.NewV7()}
	e.accounts.Create(context.Background(), e.acc, "")
	tokens, err := auth.IssueSession(context.Background(), e.sessions, session.NewSigner(testKeys), e.sid, e.acc, "Pixel 9", now)
	if err != nil {
		t.Fatal(err)
	}
	e.token = tokens.RefreshToken
	return e
}

func (e *refreshEnv) refresh(token string, at time.Time) (session.Tokens, error) {
	return auth.Refresh(context.Background(), e.sessions, e.accounts, session.NewSigner(testKeys), token, at, discard)
}

func (e *refreshEnv) revoked(t *testing.T) bool {
	s, err := e.sessions.Get(context.Background(), e.sid)
	if err != nil {
		t.Fatal(err)
	}
	return s.RevokedAt() != nil
}

func TestRefreshRotatesAndKeepsAuthTime(t *testing.T) {
	e := newRefreshEnv(t)
	out, err := e.refresh(e.token, now.Add(time.Hour))
	if err != nil || out.RefreshToken == e.token {
		t.Fatalf("%+v %v", out, err)
	}
	p, err := session.NewVerifier(testKeys).Verify(out.AccessToken, now.Add(time.Hour))
	if err != nil || p.SessionID != e.sid || !p.AuthTime.Equal(now) {
		t.Fatalf("%+v %v", p, err)
	}
	s, _ := e.sessions.Get(context.Background(), e.sid)
	if !bytes.Equal(s.PrevRefreshHash(), session.HashRefresh(e.token)) {
		t.Fatal("previous hash not kept")
	}
}

func TestRefreshReplayRevokes(t *testing.T) {
	e := newRefreshEnv(t)
	next, _ := e.refresh(e.token, now)
	if _, err := e.refresh(e.token, now); !errors.Is(err, session.ErrRevoked) || !e.revoked(t) {
		t.Fatalf("replay: %v", err)
	}
	if _, err := e.refresh(next.RefreshToken, now); !errors.Is(err, session.ErrRevoked) {
		t.Fatalf("the newer token after a replay: %v", err)
	}
}

func TestRefreshRefusals(t *testing.T) {
	e := newRefreshEnv(t)
	if _, err := e.refresh("rt_nope", now); !errors.Is(err, session.ErrInvalidRefresh) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := e.refresh(e.token, now.Add(session.IdleLimit)); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("idle: %v", err)
	}
	e.accounts.suspend(e.acc)
	if _, err := e.refresh(e.token, now); !errors.Is(err, account.ErrSuspended) || !e.revoked(t) {
		t.Fatalf("suspended: %v", err)
	}
}

// losingSave behaves as if another refresh with the same token saved first.
type losingSave struct{ *sessiontest.MemStore }

func (l losingSave) Save(ctx context.Context, s *session.Session, w uuid.UUID) error {
	if s.RevokedAt() == nil {
		return session.ErrConflict
	}
	return l.MemStore.Save(ctx, s, w)
}

func TestRefreshThatLosesARaceRevokes(t *testing.T) {
	e := newRefreshEnv(t)
	_, err := auth.Refresh(context.Background(), losingSave{e.sessions}, e.accounts, session.NewSigner(testKeys), e.token, now, discard)
	if !errors.Is(err, session.ErrRevoked) || !e.revoked(t) {
		t.Fatalf("%v, revoked %v", err, e.revoked(t))
	}
}
