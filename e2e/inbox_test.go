package e2e

import (
	"context"
	"regexp"
	"sync"
	"testing"
)

// inbox keeps every mail the server sends. The send step finishes before
// the request answers, so a test reads its code right after the call.
type inbox struct {
	mu   sync.Mutex
	msgs map[string][]string // address → subject and body, oldest first
}

func newInbox() *inbox { return &inbox{msgs: map[string][]string{}} }

func (b *inbox) Send(_ context.Context, to, subject, body string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs[to] = append(b.msgs[to], subject+"\n"+body)
	return nil
}

func (b *inbox) count(to string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.msgs[to])
}

var codeIn = regexp.MustCompile(`\b\d{6}\b`)

// code returns the code in the nth mail to "to", counting from 1.
func (b *inbox) code(t *testing.T, to string, nth int) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.msgs[to]) < nth {
		t.Fatalf("%s got %d mails, want at least %d", to, len(b.msgs[to]), nth)
	}
	return codeIn.FindString(b.msgs[to][nth-1])
}
