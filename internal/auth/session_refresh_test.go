package auth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

type refreshEnv struct {
	db       *pgxpool.Pool
	sessions session.Store
	accounts auth.AccountPort
	acc      uuid.UUID
	sid      uuid.UUID
	token    string
}

func newRefreshEnv(t *testing.T) *refreshEnv {
	t.Helper()
	db := pgtest.New(t)
	e := &refreshEnv{db: db, sessions: session.NewPGStore(db), accounts: accountsOn(db), acc: newAccount(t, db), sid: uuid.NewV7()}
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
	suspendAccount(t, e.db, e.acc)
	if _, err := e.refresh(e.token, now); !errors.Is(err, account.ErrSuspended) || !e.revoked(t) {
		t.Fatalf("suspended: %v", err)
	}
}

// losingSave behaves as if another refresh with the same token saved first.
type losingSave struct{ session.Store }

func (l losingSave) Save(ctx context.Context, s *session.Session, w uuid.UUID) error {
	if s.RevokedAt() == nil {
		return session.ErrConflict
	}
	return l.Store.Save(ctx, s, w)
}

func TestRefreshThatLosesARaceRevokes(t *testing.T) {
	e := newRefreshEnv(t)
	_, err := auth.Refresh(context.Background(), losingSave{e.sessions}, e.accounts, session.NewSigner(testKeys), e.token, now, discard)
	if !errors.Is(err, session.ErrRevoked) || !e.revoked(t) {
		t.Fatalf("%v, revoked %v", err, e.revoked(t))
	}
}

// logoutAllFirst behaves as if the account signed out everywhere just before
// the refresh saved.
type logoutAllFirst struct{ session.Store }

func (l logoutAllFirst) Save(ctx context.Context, s *session.Session, w uuid.UUID) error {
	if err := l.Store.RevokeAll(ctx, s.AccountID(), now); err != nil {
		return err
	}
	return l.Store.Save(ctx, s, w)
}

// A refresh that loses to logout-all is a signed-out session, not a token two
// parties hold: no security event is logged.
func TestRefreshThatLosesToLogoutAllIsNotReuse(t *testing.T) {
	e := newRefreshEnv(t)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	_, err := auth.Refresh(context.Background(), logoutAllFirst{e.sessions}, e.accounts, session.NewSigner(testKeys), e.token, now, log)
	if !errors.Is(err, session.ErrRevoked) || strings.Contains(logs.String(), "reuse") {
		t.Fatalf("%v, logs %q", err, logs.String())
	}
}
