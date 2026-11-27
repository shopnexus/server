package worker_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/events"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// reviews answers each review's first facts on its first read and its later
// facts, when set, on every read after.
type reviews struct {
	trustsvc.Service

	mu           *sync.Mutex
	reads        map[uuid.UUID]int
	first, later map[uuid.UUID]trustsvc.ReviewFacts
}

func (r reviews) ReviewFacts(_ context.Context, id uuid.UUID) (trustsvc.ReviewFacts, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads[id]++
	if f, ok := r.later[id]; ok && r.reads[id] > 1 {
		return f, true, nil
	}
	f, ok := r.first[id]
	return f, ok, nil
}

// A hidden review tells its subject once it shows. The second side's
// review tells both subjects, and the first, waking, tells no one again.
// A reply tells the reviewer. UC-SHR-03 A5.
func TestReviewNotices(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil, nil)
	buyer, seller, other := accounttest.New(t, e.db), accounttest.New(t, e.db), accounttest.New(t, e.db)
	order, otherOrder := uuid.NewV7(), uuid.NewV7()
	now := time.Now()
	hidden := trustsvc.ReviewFacts{
		ID: uuid.NewV7(), OrderID: otherOrder, SubjectID: other, ReviewerID: buyer, ReviewerName: "Hoàng Nam",
		ListingTitle: "Xe đạp Asama", Stars: 4, VisibleAt: now.Add(time.Second), HiddenFor: time.Second,
	}
	first := trustsvc.ReviewFacts{
		ID: uuid.NewV7(), OrderID: order, SubjectID: seller, SubjectName: "Hoàng Nam", ReviewerID: buyer, ReviewerName: "Hoàng Nam",
		ListingTitle: "Xe đạp Asama", Stars: 5, VisibleAt: now.Add(2 * time.Second), HiddenFor: 2 * time.Second,
	}
	second := trustsvc.ReviewFacts{
		ID: uuid.NewV7(), OrderID: order, SubjectID: buyer, ReviewerID: seller, ReviewerName: "Hoàng Nam",
		ListingTitle: "Xe đạp Asama", Stars: 3, VisibleAt: now, Revealed: first.ID,
	}
	revealed := first
	revealed.VisibleAt, revealed.HiddenFor, revealed.RevealedBy = now, 0, second.ID
	e.worker.Trust = reviews{
		mu: &sync.Mutex{}, reads: map[uuid.UUID]int{},
		first: map[uuid.UUID]trustsvc.ReviewFacts{hidden.ID: hidden, first.ID: first, second.ID: second},
		later: map[uuid.UUID]trustsvc.ReviewFacts{hidden.ID: hidden, first.ID: revealed},
	}
	e.start(t)
	send := func(f trustsvc.ReviewFacts) {
		t.Helper()
		ev := events.Event{ID: f.ID, Kind: events.ReviewPosted, SubjectID: f.ID, OccurredAt: now}
		if _, err := ingress.ServiceSend[events.Event](e.client, e.worker.ServiceName(), "ReviewPosted").
			Send(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	send(hidden)
	send(first)
	// The first review's handler has read it hidden before the second lands.
	time.Sleep(500 * time.Millisecond)
	e.deliver(t, "ReviewPosted", events.Event{ID: second.ID, Kind: events.ReviewPosted, SubjectID: second.ID, OccurredAt: now})

	got := e.noticesOf(t, other, 1)
	if got[0].Type != domain.TypeReview || got[0].Text != "Hoàng Nam chấm bạn 4 sao · Xe đạp Asama" || got[0].Target.Kind != "order" ||
		got[0].Target.ID != otherOrder.String() {
		t.Fatalf("hidden review: %+v", got[0])
	}
	for who, stars := range map[uuid.UUID]string{seller: "5", buyer: "3"} {
		if n := e.noticesOf(t, who, 1); n[0].Text != "Hoàng Nam chấm bạn "+stars+" sao · Xe đạp Asama" {
			t.Fatalf("%v: %+v", who, n[0])
		}
	}
	// Past the first review's deadline its handler has woken and told no one.
	time.Sleep(2 * time.Second)
	e.noticesOf(t, seller, 1)

	e.deliver(t, "ReviewReplied", events.Event{ID: uuid.NewV7(), Kind: events.ReviewReplied, SubjectID: first.ID, OccurredAt: now})
	replied := e.noticesOf(t, buyer, 2)
	if replied[0].Type != domain.TypeReviewReply || replied[0].Text != "Hoàng Nam trả lời đánh giá của bạn · Xe đạp Asama" ||
		replied[0].Target.ID != order.String() {
		t.Fatalf("reply: %+v", replied[0])
	}
}
