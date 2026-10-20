package auth_test

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"time"

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
