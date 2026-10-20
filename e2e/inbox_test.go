package e2e

import (
	"context"
	"regexp"
	"sync"
	"testing"

	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
)

// inbox keeps every message the server sends. The send step finishes before
// the OTP request answers, so a test reads its code right after the call.
type inbox struct {
	mu   sync.Mutex
	msgs map[string][]string // destination → texts, oldest first
}

func newInbox() *inbox { return &inbox{msgs: map[string][]string{}} }

type smsInbox struct{ *inbox }

func (b smsInbox) Send(_ context.Context, to, text string) error { b.add(to, text); return nil }

type emailInbox struct{ *inbox }

func (b emailInbox) Send(_ context.Context, to, _, body string) error { b.add(to, body); return nil }

func (b *inbox) sms() sms.Sender     { return smsInbox{b} }
func (b *inbox) email() email.Sender { return emailInbox{b} }

func (b *inbox) add(to, text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs[to] = append(b.msgs[to], text)
}

func (b *inbox) count(to string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.msgs[to])
}

var codeIn = regexp.MustCompile(`\b[0-9]{6}\b`)

// code returns the code in the nth message to "to", counting from 1.
func (b *inbox) code(t *testing.T, to string, nth int) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.msgs[to]) < nth {
		t.Fatalf("%s got %d messages, want at least %d", to, len(b.msgs[to]), nth)
	}
	return codeIn.FindString(b.msgs[to][nth-1])
}
