package service_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
	"uuid"
)

func TestMessageFacts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	l := e.listing(t, seller)
	long := strings.Repeat("ạ", 150)
	c := e.open(t, buyer, l, long)
	m := e.messages(t, buyer, c.ID)[0]
	f, ok, err := e.chat.MessageFacts(ctx, m.ID)
	if err != nil || !ok || f.SenderID != buyer || f.RecipientID != seller || f.ConversationID != c.ID ||
		f.SenderName == "" || utf8.RuneCountInString(f.Text) != 100 || f.Photos != 0 {
		t.Fatalf("%+v %v %v", f, ok, err)
	}
	if _, ok, err = e.chat.MessageFacts(ctx, uuid.NewV7()); err != nil || ok {
		t.Fatalf("an unknown message: %v %v", ok, err)
	}
}
