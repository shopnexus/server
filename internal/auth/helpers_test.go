package auth_test

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/config"
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

// fakeAccounts stands in for module account behind AccountPort.
type fakeAccounts struct {
	mu     sync.Mutex
	status map[uuid.UUID]account.Status
}

func newFakeAccounts() *fakeAccounts { return &fakeAccounts{status: map[uuid.UUID]account.Status{}} }

func (f *fakeAccounts) Create(_ context.Context, id uuid.UUID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.status[id]; !ok {
		f.status[id] = account.StatusActive
	}
	return nil
}

func (f *fakeAccounts) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.status, id)
	return nil
}

func (f *fakeAccounts) Status(_ context.Context, id uuid.UUID) (account.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.status[id]
	if !ok {
		return "", account.ErrNotFound
	}
	return st, nil
}

func (f *fakeAccounts) suspend(id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[id] = account.StatusSuspended
}
