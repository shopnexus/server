package service_test

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	accountrepo "github.com/shopnexus/server2/internal/account/repository"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/contact"
)

var (
	now      = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	testKeys = func() config.AuthConfig {
		k := config.NewAuthKeys()
		k.Issuer, k.Audience = "server2-test", "shopnexus-app-test"
		return k
	}()
	clientIP = netip.MustParseAddr("203.0.113.7")
	discard  = slog.New(slog.DiscardHandler)
)

func codes() domain.Codes { return domain.NewCodes(testKeys.OTPKey) }

func netipOf(s string) netip.Addr { return netip.MustParseAddr(s) }

type message struct{ To, Text string }

// inbox records what the SMS and email senders were asked to send.
type inbox struct {
	mu   sync.Mutex
	msgs []message
}

func (b *inbox) Send(_ context.Context, to, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs = append(b.msgs, message{to, text})
	return nil
}

func (b *inbox) all() []message {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]message(nil), b.msgs...)
}

// emailInbox adapts inbox to email.Sender.
type emailInbox struct{ *inbox }

func (e emailInbox) Send(ctx context.Context, to, _, body string) error {
	return e.inbox.Send(ctx, to, body)
}

// accountsOn is module account's adapter on db, reading statuses at now.
func accountsOn(db *pgxpool.Pool) accountsvc.Service {
	return accountsvc.New(accountrepo.NewAccounts(db), func() time.Time { return now })
}

// newAccount creates an active account in db and returns its ID.
func newAccount(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if err := accountsOn(db).Create(context.Background(), id, ""); err != nil {
		t.Fatal(err)
	}
	return id
}

// suspendAccount marks an account suspended, as an admin would.
func suspendAccount(t *testing.T, db *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(context.Background(),
		`UPDATE accounts SET status = 'suspended', suspend_reason = 'test' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
}

func phone(t *testing.T, raw string) contact.Address {
	t.Helper()
	a, err := contact.ParsePhone(raw)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// stored creates a login challenge in s and returns it with its code.
func stored(t *testing.T, s repository.Challenges, raw string, at time.Time) (c domain.Challenge, code string) {
	t.Helper()
	c = service.NewChallenge(codes(), uuid.NewV7(), domain.PurposeLogin, phone(t, raw), clientIP, at)
	if err := s.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c, codes().For(c.ID())
}

func hashOf(c domain.Challenge, code string) []byte { return codes().Hash(c.Destination(), code) }

// wrong returns a six-digit code that is not code.
func wrong(code string) string {
	if code == "000000" {
		return "000001"
	}
	return "000000"
}

// authWith is Auth on sessions and accounts, signing with testKeys, its log
// on log and its clock stopped at the given time.
func authWith(sessions repository.Sessions, accounts accountsvc.Service, at time.Time, log *slog.Logger) *service.Auth {
	return service.New(service.Deps{
		Sessions: sessions, Signer: domain.NewSigner(testKeys), Accounts: accounts, Now: func() time.Time { return at }, Log: log,
	})
}
