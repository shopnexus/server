package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

type refreshEnv struct {
	db       *pgxpool.Pool
	sessions repository.Sessions
	accounts repository.Accounts
	acc      uuid.UUID
	sid      uuid.UUID
	token    string
}

func newRefreshEnv(t *testing.T) *refreshEnv {
	t.Helper()
	db := pgtest.New(t)
	e := &refreshEnv{db: db, sessions: repository.NewSessions(db), accounts: repository.NewAccounts(db), acc: accounttest.New(t, db), sid: uuid.NewV7()}
	tokens, err := service.IssueSession(at(nil, e.sessions, now), context.Background(), e.sid, e.acc, "Pixel 9")
	if err != nil {
		t.Fatal(err)
	}
	e.token = tokens.RefreshToken
	return e
}

func (e *refreshEnv) refresh(token string, when time.Time) (domain.Tokens, error) {
	return at(e.accounts, e.sessions, when).Refresh(context.Background(), token)
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
	if _, err := e.refresh(e.token, now); !errors.Is(err, domain.ErrSessionRevoked) || !e.revoked(t) {
		t.Fatalf("replay: %v", err)
	}
	if _, err := e.refresh(next.RefreshToken, now); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("the newer token after a replay: %v", err)
	}
}

func TestRefreshRefusals(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	if _, err := e.refresh("rt_nope", now); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := e.refresh(e.token, now.Add(domain.SessionIdleLimit)); !errors.Is(err, domain.ErrSessionExpired) {
		t.Fatalf("idle: %v", err)
	}
	suspendAccount(t, e.db, e.acc)
	if _, err := e.refresh(e.token, now); !errors.Is(err, domain.ErrSuspended) || !e.revoked(t) {
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
	_, err := at(e.accounts, losingSave{e.sessions}, now).Refresh(context.Background(), e.token)
	if !errors.Is(err, domain.ErrSessionRevoked) || !e.revoked(t) {
		t.Fatalf("%v, revoked %v", err, e.revoked(t))
	}
}

// revokeAllFirst behaves as if the account signed out everywhere just before
// the refresh saved.
type revokeAllFirst struct{ repository.Sessions }

func (l revokeAllFirst) Save(ctx context.Context, s *domain.Session, w uuid.UUID) error {
	if err := l.Sessions.RevokeAll(ctx, s.AccountID(), now); err != nil {
		return err
	}
	return l.Sessions.Save(ctx, s, w)
}

// A refresh that loses to DeleteAllSessions is a signed-out session, not a
// token two parties hold: no security event is logged.
func TestRefreshThatLosesToDeleteAllSessionsIsNotReuse(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	svc := service.New(service.Deps{
		Accounts: e.accounts, Sessions: revokeAllFirst{e.sessions}, Signer: domain.NewSigner(testKeys), Now: func() time.Time { return now }, Log: log,
	})
	_, err := svc.Refresh(context.Background(), e.token)
	if !errors.Is(err, domain.ErrSessionRevoked) || strings.Contains(logs.String(), "reuse") {
		t.Fatalf("%v, logs %q", err, logs.String())
	}
}

// TestParallelRefreshRevokes is the spec's parallel-refresh attack: one token
// used five times at once yields at most one success, and afterwards the
// session no longer refreshes.
func TestParallelRefreshRevokes(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	type result struct {
		tokens domain.Tokens
		err    error
	}
	results := make(chan result, 5)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			out, err := e.refresh(e.token, now)
			results <- result{out, err}
		})
	}
	wg.Wait()
	close(results)
	var winners []domain.Tokens
	for r := range results {
		switch {
		case r.err == nil:
			winners = append(winners, r.tokens)
		case errors.Is(r.err, domain.ErrSessionRevoked):
		default:
			t.Fatalf("unexpected %v", r.err)
		}
	}
	if len(winners) > 1 {
		t.Fatalf("%d refreshes won", len(winners))
	}
	for _, w := range winners {
		if _, err := e.refresh(w.RefreshToken, now); !errors.Is(err, domain.ErrSessionRevoked) {
			t.Fatalf("the winner's token after the race: %v", err)
		}
	}
	if !e.revoked(t) {
		t.Fatal("the session survived the race")
	}
}
