package service_test

import (
	"context"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/service"
)

// A ShopNexus link in a message comes back as a card; an order shows only
// to its sides and ShopNexus, and a link to anything else stays text.
func TestRefs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	order := uuid.New()
	deps := e.deps
	deps.LinkHosts = []string{"shopnexus.id.vn"}
	deps.Orders = func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]service.OrderRef, error) {
		out := map[uuid.UUID]service.OrderRef{}
		for _, id := range ids {
			if id == order {
				out[id] = service.OrderRef{BuyerID: stranger, SellerID: seller, ListingID: listing, Code: "SN1", Status: "pending", Price: 100}
			}
		}
		return out, nil
	}
	chat := service.New(deps)
	c := e.open(t, buyer, listing, "Còn không?")
	text := "Món này https://shopnexus.id.vn/listings/" + listing.String() +
		" của https://shopnexus.id.vn/sellers/" + seller.String() +
		", đơn https://shopnexus.id.vn/orders/" + order.String() +
		", tìm https://shopnexus.id.vn/search?q=iphone và https://shopnexus.id.vn/listings/" + uuid.New().String()
	m, err := chat.Send(ctx, buyer, c.ID, key(), service.SendInput{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[domain.LinkKind]service.RefView{}
	for _, r := range m.Refs {
		kinds[r.Kind] = r
	}
	if r := kinds[domain.LinkListing]; r.Listing == nil || r.Listing.ID != listing || r.URL != "https://shopnexus.id.vn/listings/"+listing.String() {
		t.Errorf("listing card: %+v", r)
	}
	if r := kinds[domain.LinkSeller]; r.Seller == nil || r.Seller.ID != seller {
		t.Errorf("seller card: %+v", r)
	}
	if r := kinds[domain.LinkSearch]; r.Search == nil || r.Search.Query != "iphone" {
		t.Errorf("search card: %+v", r)
	}
	if _, ok := kinds[domain.LinkOrder]; ok {
		t.Error("buyer is not a side of the order, got its card")
	}
	if len(m.Refs) != 3 {
		t.Errorf("an unknown listing got a card: %+v", m.Refs)
	}
	// The order's seller sees its card, and so does ShopNexus.
	for _, viewer := range []uuid.UUID{seller, accountdomain.ShopNexusID} {
		got := service.RefsFor(t, chat, viewer, []service.MessageView{{Text: text}})
		found := false
		for _, r := range got[0].Refs {
			if r.Kind == domain.LinkOrder && r.Order != nil && r.Order.Code == "SN1" && r.Order.ThumbURL == kinds[domain.LinkListing].Listing.ThumbURL {
				found = true
			}
		}
		if !found {
			t.Errorf("viewer %s: no order card in %+v", viewer, got[0].Refs)
		}
	}
}
