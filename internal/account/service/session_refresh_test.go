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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
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

// The refresh that revoked a suspended account's session may lose its
// answer; the client's retry with the same token is still told of the
// suspension, and only of it while the suspension lasts. A session that
// ended for another reason keeps answering session_revoked.
func TestRefreshOfASuspendedAccountAnswersItsRetries(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	signedOut, err := service.IssueSession(at(nil, e.sessions, now), context.Background(), uuid.NewV7(), e.acc, "Pixel 8")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.NewVerifier(testKeys).Verify(signedOut.AccessToken, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = at(nil, e.sessions, now).DeleteCurrentSession(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	suspendAccount(t, e.db, e.acc)
	for i := range 2 {
		if _, err = e.refresh(e.token, now); !errors.Is(err, domain.ErrSuspended) || !e.revoked(t) {
			t.Fatalf("refresh %d: %v", i+1, err)
		}
	}
	if s, _ := e.sessions.Get(context.Background(), e.sid); s.RevokeReason() != domain.RevokedSuspended {
		t.Fatalf("reason %q", s.RevokeReason())
	}
	if _, err = e.refresh(signedOut.RefreshToken, now); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("a session signed out before the suspension: %v", err)
	}
	if _, err = e.db.Exec(context.Background(), `UPDATE accounts SET status = 'active', suspend_reason = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": e.acc}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.refresh(e.token, now); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("after the suspension: %v", err)
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

// reauthenticatedMeanwhile behaves as if a reauthentication of the session,
// at the time in at, committed after the refresh found it and before it
// saved.
type reauthenticatedMeanwhile struct {
	repository.Sessions

	at time.Time
}

func (r reauthenticatedMeanwhile) FindByRefresh(ctx context.Context, hash []byte) (domain.Session, error) {
	s, err := r.Sessions.FindByRefresh(ctx, hash)
	if err != nil {
		return s, err
	}
	if _, err = r.Reauthenticate(ctx, s.ID(), r.at); err != nil {
		return domain.Session{}, err
	}
	return s, nil
}

// A refresh that read the session before a reauthentication signs with the
// new auth_time, so the app does not store a token that answers 428 again.
func TestRefreshRacingAReauthenticationSignsTheNewAuthTime(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	when := now.Add(20 * time.Minute)
	out, err := at(e.accounts, reauthenticatedMeanwhile{Sessions: e.sessions, at: when}, when).Refresh(context.Background(), e.token)
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.NewVerifier(testKeys).Verify(out.AccessToken, when)
	if err != nil || !p.AuthTime.Equal(when) {
		t.Fatalf("auth_time %v, want %v: %v", p.AuthTime, when, err)
	}
	if e.revoked(t) {
		t.Fatal("the refresh took the reauthentication for a reused token")
	}
}

// othersSignedOutFirst behaves as if another device of the account signed
// out its other devices just before the refresh saved.
type othersSignedOutFirst struct{ repository.Sessions }

func (l othersSignedOutFirst) Save(ctx context.Context, s *domain.Session, w uuid.UUID) error {
	if err := l.Sessions.RevokeOthers(ctx, s.AccountID(), uuid.NewV7(), now); err != nil {
		return err
	}
	return l.Sessions.Save(ctx, s, w)
}

// A refresh that loses to DeleteOtherSessions is a signed-out session, not a
// token two parties hold: no security event is logged.
func TestRefreshThatLosesToDeleteOtherSessionsIsNotReuse(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	svc := service.New(service.Deps{
		Accounts: e.accounts, Sessions: othersSignedOutFirst{e.sessions}, Signer: domain.NewSigner(testKeys), Now: func() time.Time { return now }, Log: log,
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

// A session a sign-in issued while Erase ran, which Erase never saw, does
// not refresh: it is revoked.
func TestRefreshRevokesASessionOfADeletedAccount(t *testing.T) {
	t.Parallel()
	e := newRefreshEnv(t)
	if _, err := e.db.Exec(context.Background(), `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": e.acc}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.refresh(e.token, now); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("refresh: %v", err)
	}
	if !e.revoked(t) {
		t.Fatal("the session lives")
	}
}
