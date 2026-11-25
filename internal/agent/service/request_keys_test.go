package service_test

import (
	"context"
	"net/http"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/agent/service"
)

// A repeated Idempotency-Key answers the first request's conversation,
// turn, steer and skill, and starts no second run. UC-MOD-08.
func TestRequestKeysAnswerTheFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	release := make(chan struct{})
	f := &fakeAgent{run: func(req runRequest, w http.ResponseWriter) {
		lines(w, `{"kind":"event","type":"turn.started","body":{}}`)
		if req.Question == "hold" {
			<-release
		}
		lines(w, succeeded)
	}}
	s, db, lan := runsOn(t, f)
	key := uuid.NewV7()
	c, err := s.CreateConversation(ctx, lan, service.NewConversation{Key: &key})
	if err != nil {
		t.Fatal(err)
	}
	if again, againErr := s.CreateConversation(ctx, lan, service.NewConversation{Key: &key}); againErr != nil || again.ID != c.ID {
		t.Errorf("the conversation again %+v %v", again, againErr)
	}
	if theirs, _ := s.CreateConversation(ctx, newMember(t, db, "moderator"), service.NewConversation{Key: &key}); theirs.ID == c.ID {
		t.Error("another member's key found lan's conversation")
	}

	ask := uuid.NewV7()
	first, err := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?", Key: &ask})
	if err != nil {
		t.Fatal(err)
	}
	if again, againErr := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?", Key: &ask}); againErr != nil || again != first {
		t.Errorf("the question again while it runs %+v %v", again, againErr)
	}
	waitFor(t, s, lan, first.TurnID, "succeeded")
	if again, againErr := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?", Key: &ask}); againErr != nil || again != first {
		t.Errorf("the question again once it ended %+v %v", again, againErr)
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("%d runs, want 1", n)
	}

	held, err := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "hold"})
	if err != nil {
		t.Fatal(err)
	}
	steerKey := uuid.NewV7()
	steer, err := s.Steer(ctx, lan, held.TurnID, "Chỉ Hà Nội", &steerKey)
	if err != nil {
		t.Fatal(err)
	}
	if again, againErr := s.Steer(ctx, lan, held.TurnID, "Chỉ Hà Nội", &steerKey); againErr != nil || again != steer {
		t.Errorf("the steer again %+v %v", again, againErr)
	}
	queue, err := s.TurnControl(ctx, held.TurnID, 0)
	if err != nil || len(queue.Items) != 1 || *queue.Items[0].SteerID != steer.SteerID {
		t.Errorf("the queue %+v %v", queue, err)
	}
	close(release)
	waitFor(t, s, lan, held.TurnID, "succeeded")

	skillKey := uuid.NewV7()
	sk, err := s.CreateSkill(ctx, lan, service.SkillInput{Name: "Tuần", Prompt: "?", Key: &skillKey})
	if err != nil {
		t.Fatal(err)
	}
	if again, againErr := s.CreateSkill(ctx, lan, service.SkillInput{Name: "Tuần", Prompt: "?", Key: &skillKey}); againErr != nil || again.ID != sk.ID {
		t.Errorf("the skill again %+v %v", again, againErr)
	}
	if list, _ := s.Skills(ctx, lan); len(list) != 1 {
		t.Errorf("skills %+v", list)
	}
}
