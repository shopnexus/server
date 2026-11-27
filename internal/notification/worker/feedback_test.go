package worker_test

import (
	"context"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	feedbackdomain "github.com/shopnexus/server/internal/feedback/domain"
	feedbacksvc "github.com/shopnexus/server/internal/feedback/service"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/testenv"
)

type feedback struct {
	feedbacksvc.Service

	facts map[uuid.UUID]feedbacksvc.Facts
}

func (f feedback) Facts(_ context.Context, id uuid.UUID) (feedbacksvc.Facts, bool, error) {
	v, ok := f.facts[id]
	return v, ok, nil
}

// A move to shipped and a first reply each tell the owner and push, with
// no mail. A feedback gone, or back to seen by the time it is read, tells
// no one. UC-SHR-03 A2.
func TestFeedbackNotices(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	owner := accounttest.New(t, e.db)
	shipped, replied, seen, gone := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	e.worker.Feedback = feedback{facts: map[uuid.UUID]feedbacksvc.Facts{
		shipped: {ID: shipped, Owner: owner, Status: feedbackdomain.StatusShipped, Version: "2.5"},
		replied: {ID: replied, Owner: owner, Status: feedbackdomain.StatusSeen, Reply: "Cảm ơn bạn đã góp ý"},
		seen:    {ID: seen, Owner: owner, Status: feedbackdomain.StatusSeen},
	}}
	e.start(t)
	if err := e.notices.PutToken(ctx, uuid.NewV7(), owner, "owner-phone"); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	e.deliver(t, "FeedbackMoved", events.Event{ID: uuid.NewV7(), Kind: events.FeedbackMoved, SubjectID: seen, OccurredAt: at})
	e.deliver(t, "FeedbackMoved", events.Event{ID: uuid.NewV7(), Kind: events.FeedbackMoved, SubjectID: gone, OccurredAt: at})
	e.deliver(t, "FeedbackReplied", events.Event{ID: uuid.NewV7(), Kind: events.FeedbackReplied, SubjectID: seen, OccurredAt: at})
	if l, _ := e.notices.List(ctx, owner, page.Request{}, ""); len(l.Items) != 0 {
		t.Fatalf("told of nothing new: %+v", l.Items)
	}
	e.deliver(t, "FeedbackMoved", events.Event{ID: uuid.NewV7(), Kind: events.FeedbackMoved, SubjectID: shipped, OccurredAt: at})
	e.deliver(t, "FeedbackReplied", events.Event{ID: uuid.NewV7(), Kind: events.FeedbackReplied, SubjectID: replied, OccurredAt: at.Add(time.Second)})
	items := e.noticesOf(t, owner, 2)
	if items[0].Type != domain.TypeFeedbackReplied || items[0].Text != "ShopNexus trả lời góp ý của bạn: “Cảm ơn bạn đã góp ý”" ||
		items[0].Target != (domain.Target{Kind: "feedback", ID: replied.String()}) {
		t.Fatalf("reply notice %+v", items[0])
	}
	if items[1].Type != domain.TypeFeedbackMoved || items[1].Text != "Điều bạn góp ý đã có trong bản 2.5" ||
		items[1].Target != (domain.Target{Kind: "feedback", ID: shipped.String()}) {
		t.Fatalf("shipped notice %+v", items[1])
	}
	testenv.Eventually(t, 30*time.Second, func() error {
		if n := e.push.count("owner-phone"); n != 2 {
			return fmt.Errorf("%d pushes", n)
		}
		return nil
	})
	if e.push.title["owner-phone"] != "Góp ý" || e.push.data["owner-phone"]["target_kind"] != "feedback" {
		t.Fatalf("push %q %+v", e.push.title["owner-phone"], e.push.data["owner-phone"])
	}
	if e.mail.count() != 0 {
		t.Fatalf("%d mails", e.mail.count())
	}
}
