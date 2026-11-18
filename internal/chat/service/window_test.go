package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// Window gives the members and the lines oldest first; an unknown
// conversation is false.
func TestWindowOfAConversation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	chats := repository.NewChats(db)
	svc := service.New(service.Deps{Chats: chats, Now: time.Now})
	a, b := accounttest.New(t, db), accounttest.New(t, db)
	c, err := chats.OpenPair(ctx, domain.NewConversation(uuid.NewV7(), a, b, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 0, 3)
	for i, text := range []string{"Còn không bạn?", "Còn bạn", "Cọc trước nhé"} {
		sender := []uuid.UUID{a, b, b}[i]
		m, mErr := domain.NewMessage(uuid.NewV7(), c.ID, sender, text, nil, nil, nil, false, time.Now())
		if mErr != nil {
			t.Fatal(mErr)
		}
		if mErr = chats.AddMessage(ctx, m); mErr != nil {
			t.Fatal(mErr)
		}
		ids = append(ids, m.ID())
	}
	w, found, err := svc.Window(ctx, c.ID, uuid.UUID{}, 20)
	if err != nil || !found || w.AccountA != c.AccountA || w.AccountB != c.AccountB || len(w.Lines) != 3 ||
		w.Lines[0].ID != ids[0] || w.Lines[2].Text != "Cọc trước nhé" || w.Lines[2].SenderID != b {
		t.Fatalf("%+v %v %v", w, found, err)
	}
	if w, _, err = svc.Window(ctx, c.ID, ids[1], 20); err != nil || len(w.Lines) != 2 || w.Lines[1].ID != ids[1] {
		t.Fatalf("up to the second: %+v %v", w, err)
	}
	if _, found, err = svc.Window(ctx, uuid.NewV7(), uuid.UUID{}, 20); err != nil || found {
		t.Fatalf("unknown: %v %v", found, err)
	}
}
