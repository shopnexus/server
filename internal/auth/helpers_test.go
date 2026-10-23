package auth_test

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
	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/contact"
)

var (
	now      = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	testKeys = config.NewAuthKeys()
	clientIP = netip.MustParseAddr("203.0.113.7")
	discard  = slog.New(slog.DiscardHandler)
)

func codes() otp.Codes { return otp.NewCodes(testKeys.OTPKey) }

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
func accountsOn(db *pgxpool.Pool) auth.AccountPort {
	return account.Client{Store: account.NewPGStore(db), Now: func() time.Time { return now }}
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
func stored(t *testing.T, s otp.ChallengeStore, raw string, at time.Time) (c otp.Challenge, code string) {
	t.Helper()
	c = auth.NewChallenge(codes(), uuid.NewV7(), otp.PurposeLogin, phone(t, raw), clientIP, at)
	if err := s.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c, codes().For(c.ID())
}

func hashOf(c otp.Challenge, code string) []byte { return codes().Hash(c.Destination(), code) }

// wrong returns a six-digit code that is not code.
func wrong(code string) string {
	if code == "000000" {
		return "000001"
	}
	return "000000"
}
