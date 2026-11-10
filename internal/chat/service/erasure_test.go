package service_test

import (
	"context"
	"errors"
	"testing"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

// A conversation with a deleted account stays readable; the other member's
// reply is account_deleted, and the person shows as deleted.
func TestReplyToADeletedAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Còn không?")
	if err := e.accounts.Erase(ctx, seller); err != nil {
		t.Fatal(err)
	}
	if _, err := e.chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: "Alo?"}); !errors.Is(err, accountdomain.ErrAccountDeleted) {
		t.Fatalf("reply: %v", err)
	}
	got, err := e.chat.Get(ctx, buyer, c.ID)
	if err != nil || !got.Other.Deleted || got.Other.DisplayName != "" {
		t.Fatalf("get: %+v %v", got.Other, err)
	}
	inbox, err := e.chat.Inbox(ctx, buyer, false, page.Request{})
	if err != nil || len(inbox.Items) != 1 || !inbox.Items[0].Other.Deleted {
		t.Fatalf("inbox: %+v %v", inbox.Items, err)
	}
	if msgs := e.messages(t, buyer, c.ID); len(msgs) != 1 {
		t.Fatalf("messages: %d", len(msgs))
	}
}
