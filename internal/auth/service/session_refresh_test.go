package service_test

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
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

type refreshEnv struct {
	db       *pgxpool.Pool
	sessions repository.Sessions
	accounts accountsvc.Service
	acc      uuid.UUID
	sid      uuid.UUID
	token    string
}

func newRefreshEnv(t *testing.T) *refreshEnv {
	t.Helper()
	db := pgtest.New(t)
	e := &refreshEnv{db: db, sessions: repository.NewSessions(db), accounts: accountsOn(db), acc: newAccount(t, db), sid: uuid.NewV7()}
	tokens, err := authWith(e.sessions, nil, now, discard).IssueSession(context.Background(), e.sid, e.acc, "Pixel 9")
	if err != nil {
		t.Fatal(err)
	}
	e.token = tokens.RefreshToken
	return e
}

func (e *refreshEnv) refresh(token string, at time.Time) (domain.Tokens, error) {
	return authWith(e.sessions, e.accounts, at, discard).Refresh(context.Background(), token)
}

func (e *refreshEnv) revoked(t *testing.T) bool {
	t.Helper()
	s, err := e.sessions.Get(context.Background(), e.sid)
	if err != nil {
		t.Fatal(err)
	}
	return s.RevokedAt() != nil
}

func TestRefreshRotatesAndKeepsAuthTime(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	out, err := e.refresh(e.token, now.Add(time.Hour))
	if err != nil || out.RefreshToken == e.token {
		t.Fatalf("%+v %v", out, err)
	}
	p, err := domain.NewVerifier(testKeys).Verify(out.AccessToken, now.Add(time.Hour))
	if err != nil || p.SessionID != e.sid || !p.AuthTime.Equal(now) {
		t.Fatalf("%+v %v", p, err)
	}
	s, _ := e.sessions.Get(context.Background(), e.sid)
	if !bytes.Equal(s.PrevRefreshHash(), domain.HashRefresh(e.token)) {
		t.Fatal("previous hash not kept")
	}
}

func TestRefreshReplayRevokes(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	next, _ := e.refresh(e.token, now)
	if _, err := e.refresh(e.token, now); !errors.Is(err, domain.ErrRevoked) || !e.revoked(t) {
		t.Fatalf("replay: %v", err)
	}
	if _, err := e.refresh(next.RefreshToken, now); !errors.Is(err, domain.ErrRevoked) {
		t.Fatalf("the newer token after a replay: %v", err)
	}
}

func TestRefreshRefusals(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	if _, err := e.refresh("rt_nope", now); !errors.Is(err, domain.ErrInvalidRefresh) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := e.refresh(e.token, now.Add(domain.SessionIdleLimit)); !errors.Is(err, domain.ErrExpired) {
		t.Fatalf("idle: %v", err)
	}
	suspendAccount(t, e.db, e.acc)
	if _, err := e.refresh(e.token, now); !errors.Is(err, accountdomain.ErrSuspended) || !e.revoked(t) {
		t.Fatalf("suspended: %v", err)
	}
}

// losingSave behaves as if another refresh with the same token saved first.
type losingSave struct{ repository.Sessions }

func (l losingSave) Save(ctx context.Context, s *domain.Session, w uuid.UUID) error {
	if s.RevokedAt() == nil {
		return repository.ErrConflict
	}
	return l.Sessions.Save(ctx, s, w)
}

func TestRefreshThatLosesARaceRevokes(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	_, err := authWith(losingSave{e.sessions}, e.accounts, now, discard).Refresh(context.Background(), e.token)
	if !errors.Is(err, domain.ErrRevoked) || !e.revoked(t) {
		t.Fatalf("%v, revoked %v", err, e.revoked(t))
	}
}

// logoutAllFirst behaves as if the account signed out everywhere just before
// the refresh saved.
type logoutAllFirst struct{ repository.Sessions }

func (l logoutAllFirst) Save(ctx context.Context, s *domain.Session, w uuid.UUID) error {
	if err := l.Sessions.RevokeAll(ctx, s.AccountID(), now); err != nil {
		return err
	}
	return l.Sessions.Save(ctx, s, w)
}

// A refresh that loses to logout-all is a signed-out session, not a token two
// parties hold: no security event is logged.
func TestRefreshThatLosesToLogoutAllIsNotReuse(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	_, err := authWith(logoutAllFirst{e.sessions}, e.accounts, now, log).Refresh(context.Background(), e.token)
	if !errors.Is(err, domain.ErrRevoked) || strings.Contains(logs.String(), "reuse") {
		t.Fatalf("%v, logs %q", err, logs.String())
	}
}
