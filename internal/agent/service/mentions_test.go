package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/testenv"
)

// A question's attachments are kept on its turn, each record once with
// its label trimmed; a run is told every record its conversation
// mentions, the turn's first, without labels; and the member finds their
// own conversations that mention a record. UC-MOD-08.
func TestAskMentions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := &fakeAgent{run: func(req runRequest, w http.ResponseWriter) {
		if req.Mode == "new" && req.Question == "break" {
			lines(w, `{"kind":"checkpoint","state":{"v":1,"wrote":false,"question":"break"}}`,
				`{"kind":"event","type":"step.started","body":{"step_id":"s1","title":"Đọc"}}`)
			return
		}
		lines(w, succeeded)
	}}
	s, db, lan := runsOn(t, f)
	ticket, order, listing := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	mentioned := func(req runRequest) string {
		b, _ := json.Marshal(req.Mentions)
		return string(b)
	}
	ref := func(kind string, id uuid.UUID) string { return fmt.Sprintf(`{"kind":%q,"id":%q}`, kind, id) }

	c, err := s.CreateConversation(ctx, lan, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "Ticket này sao?", Attachments: []service.Attachment{
		{Kind: "ticket", ID: ticket, Label: "  Ticket #12  "}, {Kind: "ticket", ID: ticket, Label: "again"}, {Kind: "order", ID: order},
	}})
	if err != nil {
		t.Fatal(err)
	}
	v := waitFor(t, s, lan, first.TurnID, "succeeded")
	want := []service.Attachment{{Kind: "ticket", ID: ticket, Label: "Ticket #12"}, {Kind: "order", ID: order}}
	if fmt.Sprint(v.Attachments) != fmt.Sprint(want) {
		t.Errorf("the turn's attachments %+v", v.Attachments)
	}
	if got, wantRefs := mentioned(f.requests()[0]), "["+ref("ticket", ticket)+","+ref("order", order)+"]"; got != wantRefs {
		t.Errorf("the first run's mentions %s, want %s", got, wantRefs)
	}
	second, err := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "Còn tin đăng?", Attachments: []service.Attachment{
		{Kind: "listing", ID: listing}, {Kind: "ticket", ID: ticket},
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, lan, second.TurnID, "succeeded")
	if got, wantRefs := mentioned(f.requests()[1]), "["+ref("listing", listing)+","+ref("ticket", ticket)+","+ref("order", order)+"]"; got != wantRefs {
		t.Errorf("the second run's mentions %s, want %s", got, wantRefs)
	}
	detail, err := s.Conversation(ctx, lan, c.ID)
	if err != nil || len(detail.Turns) != 2 || len(detail.Turns[0].Attachments) != 2 || len(detail.Turns[1].Attachments) != 2 {
		t.Errorf("the conversation's turns %+v %v", detail.Turns, err)
	}

	// A resumed segment is told the same records.
	broken, err := s.CreateConversation(ctx, lan, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.Ask(ctx, lan, broken.ID, service.NewTurn{Text: "break", Attachments: []service.Attachment{{Kind: "account", ID: order}}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, lan, started.TurnID, "failed")
	if _, err = s.Control(ctx, lan, started.TurnID, service.ControlContinue); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, lan, started.TurnID, "succeeded")
	testenv.Eventually(t, 5*time.Second, func() error {
		reqs := f.requests()
		last := reqs[len(reqs)-1]
		if last.Mode != "resume" || mentioned(last) != "["+ref("account", order)+"]" {
			return fmt.Errorf("the resume %s %s", last.Mode, mentioned(last))
		}
		return nil
	})

	eleven := make([]service.Attachment, 11)
	for i := range eleven {
		eleven[i] = service.Attachment{Kind: "ticket", ID: uuid.NewV7()}
	}
	for name, in := range map[string][]service.Attachment{
		"an unknown kind": {{Kind: "message", ID: ticket}},
		"a long label":    {{Kind: "ticket", ID: ticket, Label: strings.Repeat("ê", 121)}},
		"eleven":          eleven,
	} {
		if _, err = s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?", Attachments: in}); !errors.Is(err, domain.ErrRequestInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// The lists by mention: the caller's own active conversations only.
	if _, err = s.CreateConversation(ctx, lan, service.NewConversation{}); err != nil {
		t.Fatal(err)
	}
	other := newMember(t, db, "moderator")
	theirs, err := s.CreateConversation(ctx, other, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Ask(ctx, other, theirs.ID, service.NewTurn{Text: "?", Attachments: []service.Attachment{{Kind: "ticket", ID: ticket}}}); err != nil {
		t.Fatal(err)
	}
	if list, listErr := s.Conversations(ctx, lan, service.ConversationQuery{MentionKind: "ticket", MentionID: &ticket}); listErr != nil ||
		len(list) != 1 || list[0].ID != c.ID {
		t.Errorf("lan's that mention the ticket %+v %v", list, listErr)
	}
	if list, _ := s.Conversations(ctx, lan, service.ConversationQuery{MentionKind: "account", MentionID: &ticket}); len(list) != 0 {
		t.Errorf("another kind with the same id %+v", list)
	}
	if list, _ := s.Conversations(ctx, lan, service.ConversationQuery{}); len(list) != 3 {
		t.Errorf("lan's own %+v", list)
	}
	for name, q := range map[string]service.ConversationQuery{
		"a kind alone":    {MentionKind: "ticket"},
		"an id alone":     {MentionID: &ticket},
		"an unknown kind": {MentionKind: "message", MentionID: &ticket},
		"with a subject":  {MentionKind: "ticket", MentionID: &ticket, SubjectKind: domain.SubjectTicket},
	} {
		if _, err = s.Conversations(ctx, lan, q); !errors.Is(err, domain.ErrRequestInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
