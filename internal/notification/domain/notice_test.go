package domain_test

import (
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/notification/domain"
)

var (
	at     = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	buyer  = uuid.NewV7()
	seller = uuid.NewV7()
)

func facts(moves ...domain.Move) domain.DealFacts {
	return domain.DealFacts{
		DealID: uuid.NewV7(), BuyerID: buyer, SellerID: seller,
		Title: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam", Moves: moves,
		Carrier: "ghn", TrackingCode: "8A2K9",
	}
}

func TestFromMoves(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		move   domain.Move
		reason string
		to     uuid.UUID
		typ    domain.Type
	}{
		"an offer":                    {domain.Move{Kind: "offer", By: "buyer", Amount: 1_200_000}, "", seller, domain.TypeOffer},
		"a counter":                   {domain.Move{Kind: "counter", By: "seller", Amount: 1_400_000}, "", buyer, domain.TypeCounter},
		"the buyer accepts":           {domain.Move{Kind: "accept", By: "buyer", Amount: 1_300_000}, "", seller, domain.TypeAgreed},
		"the seller accepts":          {domain.Move{Kind: "accept", By: "seller", Amount: 1_300_000}, "", buyer, domain.TypeAgreed},
		"the seller declines":         {domain.Move{Kind: "decline", By: "seller"}, "", buyer, domain.TypeDeclined},
		"the buyer cancels":           {domain.Move{Kind: "cancel", By: "buyer"}, "", seller, domain.TypeAgreementCancelled},
		"a new price":                 {domain.Move{Kind: "price_changed", By: "system"}, "", buyer, domain.TypeOfferVoided},
		"offers closed":               {domain.Move{Kind: "offers_closed", By: "system"}, "", buyer, domain.TypeOfferVoided},
		"the listing closed":          {domain.Move{Kind: "listing_closed", By: "system"}, "", buyer, domain.TypeOfferVoided},
		"a COD request":               {domain.Move{Kind: "cod_requested", By: "buyer"}, "", seller, domain.TypeCodRequested},
		"shipped":                     {domain.Move{Kind: "shipped", By: "seller"}, "", buyer, domain.TypeShipped},
		"delivered":                   {domain.Move{Kind: "delivered", By: "seller"}, "", buyer, domain.TypeDelivered},
		"returned":                    {domain.Move{Kind: "returned", By: "seller"}, "", buyer, domain.TypeReturned},
		"the buyer cancels an order":  {domain.Move{Kind: "order_cancelled", By: "buyer"}, "buyer", seller, domain.TypeOrderCancelled},
		"the seller cancels an order": {domain.Move{Kind: "order_cancelled", By: "seller"}, "seller", buyer, domain.TypeOrderCancelled},
		"sent to another":             {domain.Move{Kind: "order_cancelled", By: "system"}, "sent_to_another", buyer, domain.TypeOrderCancelled},
	} {
		f := facts(c.move)
		f.CancelReason = c.reason
		got := domain.FromMoves(f, at)
		if len(got) != 1 || got[0].Recipient != c.to || got[0].Type != c.typ || got[0].CollapseKey != "deal:"+f.DealID.String() ||
			!got[0].At.Equal(at) {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got := domain.FromMoves(facts(domain.Move{Kind: "withdraw", By: "buyer"}), at); len(got) != 0 {
		t.Errorf("a withdrawal notifies: %+v", got)
	}
}

func TestFollowedListing(t *testing.T) {
	t.Parallel()
	listing := uuid.NewV7()
	n := domain.FollowedListing(buyer, seller, "Hoàng Nam", listing, "Xe đạp Asama", at)
	if n.Recipient != buyer || n.Type != domain.TypeFollowedListing || n.CollapseKey != "seller:"+seller.String() {
		t.Fatalf("%+v", n)
	}
	if got := domain.TargetOf(n.Type, n.Params); got.Kind != "listing" || got.ID != listing.String() {
		t.Fatalf("target %+v", got)
	}
	if domain.Title(n.Type) != "Tin đăng" {
		t.Fatalf("title %q", domain.Title(n.Type))
	}
}

func TestText(t *testing.T) {
	t.Parallel()
	offer := domain.FromMoves(facts(domain.Move{Kind: "offer", By: "buyer", Amount: 1_200_000}), at)[0]
	for want, n := range map[string]domain.Notice{
		"Đức Huy trả 1,2 tr cho Xe đạp Asama":       offer,
		"Hoàng Nam đã gửi Xe đạp Asama · GHN 8A2K9": domain.FromMoves(facts(domain.Move{Kind: "shipped", By: "seller"}), at)[0],
		"Đức Huy trả 350.000 ₫ cho Xe đạp Asama":    domain.FromMoves(facts(domain.Move{Kind: "offer", By: "buyer", Amount: 350_000}), at)[0],
		"Tài khoản của bạn bị khoá đến 3/11":        domain.Suspended(buyer, "2026-11-03T08:00:00Z", at),
		"Tài khoản của bạn bị khoá":                 domain.Suspended(buyer, "", at),
		"Đức Huy chấm 5 sao cho Xe đạp Asama":       domain.Reviewed(seller, "Đức Huy", "Xe đạp Asama", 5, at),
		"Hoàng Nam vừa đăng tin mới: Xe đạp Asama":  domain.FollowedListing(buyer, seller, "Hoàng Nam", uuid.NewV7(), "Xe đạp Asama", at),
	} {
		if got := domain.Text(n.Type, n.Params); got != want {
			t.Errorf("%s: %q", want, got)
		}
	}
	if offer.Params.Carrier != "" || offer.Params.Code != "" {
		t.Errorf("an offer stores the waybill: %+v", offer.Params)
	}
	if got := domain.TargetOf(offer.Type, offer.Params); got.Kind != "deal" || got.ID != offer.Params.DealID.String() {
		t.Errorf("target: %+v", got)
	}
	if got := domain.Title(offer.Type); got != "Trả giá" {
		t.Errorf("title: %q", got)
	}
	suspended := domain.Suspended(buyer, "", at)
	if got := domain.TargetOf(suspended.Type, suspended.Params); got.Kind != "account" || got.ID != "" {
		t.Errorf("account target: %+v", got)
	}
	if got := domain.ChatBody("Hoàng Nam", "Còn không bạn?", 0); got != "Hoàng Nam: Còn không bạn?" {
		t.Errorf("chat: %q", got)
	}
	if got := domain.ChatBody("Hoàng Nam", strings.Repeat("ạ", 150), 0); got != "Hoàng Nam: "+strings.Repeat("ạ", 100) {
		t.Errorf("a long text: %q", got)
	}
	if got := domain.ChatBody("Hoàng Nam", "", 2); got != "Hoàng Nam gửi 2 ảnh" {
		t.Errorf("photos: %q", got)
	}
}
