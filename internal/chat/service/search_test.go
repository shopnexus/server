package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// UC-DEAL-01 A4.
func TestSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	c := e.open(t, buyer, catalogtest.Listing(t, e.db, seller), "Giày còn ship nhanh không?")
	got, err := e.chat.Search(ctx, seller, "  GIÀY  ", page.Request{})
	if err != nil || len(got.Messages.Items) != 1 || got.Messages.Items[0].ConversationID != c.ID ||
		got.Messages.Items[0].Other.ID != buyer || got.Messages.Items[0].Message.Text != "Giày còn ship nhanh không?" {
		t.Fatalf("messages: %+v %v", got, err)
	}
	if got.Threads == nil {
		t.Fatal("threads is null on the first page")
	}
	later, err := e.chat.Search(ctx, seller, "giay", page.Request{Cursor: got.Messages.Items[0].Message.ID})
	if err != nil || later.Threads == nil || len(later.Threads) != 0 || len(later.Messages.Items) != 0 {
		t.Fatalf("a later page: %+v %v", later, err)
	}
}

// UC-DEAL-01 A4.
func TestSearchRefusesAnEmptyQuery(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, q := range []string{"", "   ", "!!! ,,", strings.Repeat("a", domain.MaxQuery+1)} {
		if _, err := e.chat.Search(context.Background(), e.person(t), q, page.Request{}); !errors.Is(err, domain.ErrInvalidQuery) {
			t.Errorf("%q: %v", q, err)
		}
	}
}
