package e2e

import (
	"context"
	"sync"
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
