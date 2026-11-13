package e2e

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/testenv"
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
	var got []push.Message
	testenv.Eventually(t, 30*time.Second, func() error {
		p.mu.Lock()
		defer p.mu.Unlock()
		if got = p.sent[token]; len(got) < n {
			return fmt.Errorf("token %s got %d pushes, want %d", token, len(got), n)
		}
		return nil
	})
	return got[n-1]
}

// waitTitled polls until token got a push titled title and returns it.
func (p *phoneBox) waitTitled(t *testing.T, token, title string) push.Message {
	t.Helper()
	var found push.Message
	testenv.Eventually(t, 30*time.Second, func() error {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, m := range p.sent[token] {
			if m.Title == title {
				found = m
				return nil
			}
		}
		return fmt.Errorf("token %s got no push titled %q", token, title)
	})
	return found
}
