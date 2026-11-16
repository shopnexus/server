package service_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/chat/service"
)

// The askers of a listing are those who wrote to the seller naming it,
// each once, since their first such message. The seller naming their own
// listing asks nothing, and a listing nobody wrote about has no entry.
func TestListingAskers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, ann, bao := e.person(t), e.person(t), e.person(t)
	listing, quiet := catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller)
	first := e.open(t, ann, listing, "Còn hàng không?")
	if _, err := e.chat.Send(ctx, ann, first.ID, key(), service.SendInput{Text: "Cho xem thêm ảnh", ListingID: &listing}); err != nil {
		t.Fatal(err)
	}
	// The seller recommends their own listing to Bao, who answers without
	// naming it: Bao asked nothing about it.
	withBao := e.open(t, bao, quiet, "Bàn này còn không?")
	if _, err := e.chat.Send(ctx, seller, withBao.ID, key(), service.SendInput{Text: "Xem chiếc xe này", ListingID: &listing}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.chat.Send(ctx, bao, withBao.ID, key(), service.SendInput{Text: "Để mình xem"}); err != nil {
		t.Fatal(err)
	}

	got, err := e.chat.ListingAskers(ctx, seller, []uuid.UUID{listing, quiet, uuid.NewV7()})
	if err != nil {
		t.Fatal(err)
	}
	if a := got[listing]; len(a) != 1 || a[0].ID != ann || a[0].Since.IsZero() {
		t.Fatalf("askers of the listing: %+v", a)
	}
	if a := got[quiet]; len(a) != 1 || a[0].ID != bao {
		t.Fatalf("askers of the other listing: %+v", a)
	}
	if len(got) != 2 {
		t.Fatalf("%d listings: %+v", len(got), got)
	}
	// Another seller reads nothing of these conversations.
	if other, readErr := e.chat.ListingAskers(ctx, ann, []uuid.UUID{listing, quiet}); readErr != nil || len(other) != 0 {
		t.Fatalf("as another account: %+v %v", other, readErr)
	}
}
