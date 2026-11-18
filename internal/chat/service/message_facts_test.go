package service_test

import (
	"context"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
)

func TestMessageFacts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	l := catalogtest.Listing(t, e.db, seller)
	long := strings.Repeat("ạ", 150)
	c := e.open(t, buyer, l, long)
	m := e.messages(t, buyer, c.ID)[0]
	f, ok, err := e.chat.MessageFacts(ctx, m.ID)
	if err != nil || !ok || f.RecipientID != seller || f.ConversationID != c.ID ||
		f.SenderName == "" || f.Text != long || f.Photos != 0 {
		t.Fatalf("%+v %v %v", f, ok, err)
	}
	if _, ok, err = e.chat.MessageFacts(ctx, uuid.NewV7()); err != nil || ok {
		t.Fatalf("an unknown message: %v %v", ok, err)
	}
}
