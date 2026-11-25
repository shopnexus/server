package worker_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/support/service"
	"github.com/shopnexus/server/internal/support/worker"
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

// recorder is a support service that records what the worker tells it.
type recorder struct {
	service.Service

	mu       *sync.Mutex
	messaged map[uuid.UUID]bool
	deleted  []uuid.UUID
}

func (r *recorder) Messaged(_ context.Context, conversation, _ uuid.UUID, fromPerson bool, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messaged[conversation] = fromPerson
	return nil
}

func (r *recorder) AccountDeleted(_ context.Context, account, _ uuid.UUID, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, account)
	return nil
}

func TestWorker(t *testing.T) {
	t.Parallel()
	person, fromShopNexus, pair := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	personConv, shopNexusConv, pairConv := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	rec := &recorder{mu: &sync.Mutex{}, messaged: map[uuid.UUID]bool{}}
	w := worker.Worker{
		Prefix: restatetest.Prefix(),
		Chats: facts{by: map[uuid.UUID]chatsvc.MessageFacts{
			person:        {ID: person, ConversationID: personConv, RecipientID: accountdomain.ShopNexusID, Kind: chatdomain.KindSupport},
			fromShopNexus: {ID: fromShopNexus, ConversationID: shopNexusConv, RecipientID: uuid.NewV7(), Kind: chatdomain.KindSupport},
			pair:          {ID: pair, ConversationID: pairConv, RecipientID: uuid.NewV7(), Kind: chatdomain.KindPair},
		}},
		Support: rec,
	}
	client := restatetest.Start(t, restate.Reflect(w))
	send := func(handler string, e events.Event) {
		t.Helper()
		if _, err := ingress.Service[events.Event, restate.Void](client, w.ServiceName(), handler).Request(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uuid.UUID{person, fromShopNexus, pair, uuid.NewV7()} {
		send("MessageSent", events.Event{ID: uuid.NewV7(), Kind: events.MessageSent, SubjectID: id, OccurredAt: time.Now()})
	}
	gone := uuid.NewV7()
	send("AccountDeleted", events.Event{ID: uuid.NewV7(), Kind: events.AccountDeleted, SubjectID: gone, OccurredAt: time.Now()})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.messaged) != 2 || !rec.messaged[personConv] || rec.messaged[shopNexusConv] {
		t.Fatalf("messaged %v", rec.messaged)
	}
	if len(rec.deleted) != 1 || rec.deleted[0] != gone {
		t.Fatalf("deleted %v", rec.deleted)
	}
}
