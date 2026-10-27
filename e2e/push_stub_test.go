package e2e

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/provider/push"
)

// phoneBox stands in for FCM: it keeps every push by token.
type phoneBox struct {
	mu   sync.Mutex
	sent map[string][]push.Message
}

func newPhones() *phoneBox { return &phoneBox{sent: map[string][]push.Message{}} }

func (p *phoneBox) Send(_ context.Context, token string, m push.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent[token] = append(p.sent[token], m)
	return nil
}

func (p *phoneBox) count(token string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sent[token])
}

// wait polls until token got n pushes and returns the last.
func (p *phoneBox) wait(t *testing.T, token string, n int) push.Message {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		p.mu.Lock()
		got := p.sent[token]
		p.mu.Unlock()
		if len(got) >= n {
			return got[n-1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("token %s got %d pushes, want %d", token, len(got), n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
