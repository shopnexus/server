package worker_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/platform/testenv"
	"github.com/shopnexus/server/internal/trust/service"
	"github.com/shopnexus/server/internal/trust/worker"
)

// facts answers MessageFacts from a map.
type facts struct {
	chatsvc.Service

	by map[uuid.UUID]chatsvc.MessageFacts
}

func (f facts) MessageFacts(_ context.Context, id uuid.UUID) (chatsvc.MessageFacts, bool, error) {
	m, ok := f.by[id]
	return m, ok, nil
}

// checks stands in for ScamCheck and records the conversations it runs.
type checks struct {
	prefix string
	mu     *sync.Mutex
	keys   *[]string
}

func (c checks) ServiceName() string { return c.prefix + service.ScamCheckName }

func (c checks) Run(ctx restate.ObjectContext) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.keys = append(*c.keys, restate.Key(ctx))
	return nil
}

// eraser records DeleteScamWarnings.
type eraser struct {
	service.Service

	mu      *sync.Mutex
	deleted *[]uuid.UUID
}

func (e eraser) DeleteScamWarnings(_ context.Context, id uuid.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	*e.deleted = append(*e.deleted, id)
	return nil
}

func TestScamWorker(t *testing.T) {
	t.Parallel()
	prefix := restatetest.Prefix()
	mu := &sync.Mutex{}
	var keys []string
	var deleted []uuid.UUID
	text, photo, support, conversation := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	w := worker.ScamWorker{
		Prefix: prefix,
		Chats: facts{by: map[uuid.UUID]chatsvc.MessageFacts{
			text:  {ID: text, ConversationID: conversation, Text: "Còn không bạn?"},
			photo: {ID: photo, ConversationID: conversation, Photos: 2},
			// A support chat gets no scam check, whatever it says.
			support: {ID: support, ConversationID: uuid.NewV7(), Text: "chuyển cọc trước", Kind: chatdomain.KindSupport},
		}},
		Trust: eraser{mu: mu, deleted: &deleted},
	}
	client := restatetest.Start(t, restate.Reflect(w), restate.Reflect(checks{prefix: prefix, mu: mu, keys: &keys}))
	send := func(handler string, e events.Event) {
		t.Helper()
		if _, err := ingress.Service[events.Event, restate.Void](client, w.ServiceName(), handler).Request(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uuid.UUID{photo, uuid.NewV7(), support, text} {
		send("MessageSent", events.Event{ID: uuid.NewV7(), Kind: events.MessageSent, SubjectID: id, OccurredAt: time.Now()})
	}
	gone := uuid.NewV7()
	send("AccountDeleted", events.Event{ID: uuid.NewV7(), Kind: events.AccountDeleted, SubjectID: gone, OccurredAt: time.Now()})
	testenv.Eventually(t, 10*time.Second, func() error {
		mu.Lock()
		defer mu.Unlock()
		if len(keys) != 1 || keys[0] != conversation.String() {
			return fmt.Errorf("checks ran for %v", keys)
		}
		return nil
	})
	mu.Lock()
	defer mu.Unlock()
	if len(deleted) != 1 || deleted[0] != gone {
		t.Fatalf("deleted %v", deleted)
	}
}
