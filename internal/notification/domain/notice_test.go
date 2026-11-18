package domain_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/notification/domain"
)

var (
	at     = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	buyer  = uuid.NewV7()
	seller = uuid.NewV7()
	// order is the order the tests' order steps are steps of.
	order = uuid.NewV7()
)

func facts(moves ...domain.Move) domain.DealFacts {
	return domain.DealFacts{
		DealID: uuid.NewV7(), BuyerID: buyer, SellerID: seller,
		Title: "Xe đạp Asama", BuyerName: "Đức Huy", SellerName: "Hoàng Nam", Moves: moves,
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
		"completed by the buyer":      {domain.Move{Kind: "completed", By: "buyer"}, "", seller, domain.TypeCompleted},
		"the buyer cancels an order":  {domain.Move{Kind: "order_cancelled", By: "buyer"}, "buyer", seller, domain.TypeOrderCancelled},
		"the seller cancels an order": {domain.Move{Kind: "order_cancelled", By: "seller"}, "seller", buyer, domain.TypeOrderCancelled},
		"shipped to another":          {domain.Move{Kind: "order_cancelled", By: "system"}, "shipped_to_another", buyer, domain.TypeOrderCancelled},
	} {
		c.move.OrderID, c.move.CancelReason = order, c.reason
		f := facts(c.move)
		got := domain.FromMoves(f, at)
		if len(got) != 1 || got[0].Recipient != c.to || got[0].Type != c.typ || got[0].CollapseKey != "deal:"+f.DealID.String() ||
			!got[0].At.Equal(at) {
			t.Errorf("%s: %+v", name, got)
		}
	}
	failed := domain.FromMoves(facts(domain.Move{Kind: "failed", By: "system"}), at)
	if len(failed) != 2 || failed[0].Recipient != seller || failed[1].Recipient != buyer ||
		failed[0].Type != domain.TypeFailed || failed[1].Type != domain.TypeFailed {
		t.Errorf("a failed delivery: %+v", failed)
	}
	if got := domain.FromMoves(facts(domain.Move{Kind: "shipment_updated", By: "system"}), at); len(got) != 0 {
		t.Errorf("a carrier's report notifies: %+v", got)
	}
	if got := domain.FromMoves(facts(domain.Move{Kind: "withdraw", By: "buyer"}), at); len(got) != 0 {
		t.Errorf("a withdrawal notifies: %+v", got)
	}
}

// Each order notice names the order its move is a step of, so two orders
// of one deal open apart; an order notice stored without its order opens
// the deal.
func TestOrderNoticesNameTheirOrder(t *testing.T) {
	t.Parallel()
	first, second := uuid.NewV7(), uuid.NewV7()
	f := facts(
		domain.Move{Kind: "order_cancelled", By: "buyer", OrderID: first, CancelReason: "buyer"},
		domain.Move{Kind: "accept", By: "buyer", Amount: 1_500_000},
		domain.Move{Kind: "cod_requested", By: "buyer", Amount: 1_500_000, OrderID: second},
	)
	got := domain.FromMoves(f, at)
	if len(got) != 3 || got[0].Params.OrderID != first || got[0].Params.Reason != "buyer" ||
		got[1].Params.OrderID != (uuid.UUID{}) || got[2].Params.OrderID != second || got[2].Params.Reason != "" {
		t.Fatalf("%+v", got)
	}
	for i, want := range []domain.Target{
		{Kind: "order", ID: first.String()}, {Kind: "deal", ID: f.DealID.String()}, {Kind: "order", ID: second.String()},
	} {
		if target := domain.TargetOf(got[i].Type, got[i].Params); target != want {
			t.Errorf("notice %d: target %+v, want %+v", i, target, want)
		}
	}
	old := domain.Params{DealID: f.DealID, Title: "Xe đạp Asama", Actor: "Hoàng Nam", Carrier: "ghn", Code: "8A2K9"}
	if target := domain.TargetOf(domain.TypeShipped, old); target.Kind != "deal" || target.ID != f.DealID.String() {
		t.Errorf("an order notice without its order: %+v", target)
	}
}

// DealMoved journals DealFacts. A journal entry from before 2026-11-17 has
// the latest order's carrier, code and cancel reason on the facts and no
// order on the moves; a replay decodes it and tells what it told then.
func TestDealFactsOfAnOldJournalEntry(t *testing.T) {
	t.Parallel()
	deal := uuid.NewV7()
	old := `{"DealID":"` + deal.String() + `","BuyerID":"` + buyer.String() + `","SellerID":"` + seller.String() + `",
"Title":"Xe đạp Asama","BuyerName":"Đức Huy","SellerName":"Hoàng Nam",
"Moves":[{"Kind":"shipped","By":"seller","Amount":0},{"Kind":"order_cancelled","By":"system","Amount":0}],
"Carrier":"ghn","TrackingCode":"8A2K9","CancelReason":"pickup_timeout"}`
	var f domain.DealFacts
	// Restate journals DealFacts by its Go field names; tags would rename them.
	if err := json.Unmarshal([]byte(old), &f); err != nil { //nolint:musttag // the journal's own encoding
		t.Fatal(err)
	}
	got := domain.FromMoves(f, at)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	for want, n := range map[string]domain.Notice{
		"Hoàng Nam đã gửi Xe đạp Asama · GHN 8A2K9":         got[0],
		"Hãng vận chuyển chưa lấy Xe đạp Asama, đơn đã huỷ": got[2],
	} {
		if text := domain.Text(n.Type, n.Params); text != want {
			t.Errorf("%s: %q", want, text)
		}
		if target := domain.TargetOf(n.Type, n.Params); target != (domain.Target{Kind: "deal", ID: deal.String()}) {
			t.Errorf("%s: target %+v", want, target)
		}
	}
	if got[1].Recipient != seller || got[1].Params.Reason != "pickup_timeout" {
		t.Errorf("the seller hears of the timeout: %+v", got[1])
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
	cancelled := func(reason string) domain.Notice {
		got := domain.FromMoves(facts(domain.Move{Kind: "order_cancelled", By: "system", OrderID: order, CancelReason: reason}), at)
		return got[len(got)-1] // the buyer's copy
	}
	for want, n := range map[string]domain.Notice{
		"Đức Huy đã nhận Xe đạp Asama": domain.FromMoves(facts(domain.Move{Kind: "completed", By: "buyer"}), at)[0],
		"Xe đạp Asama đã giao. Đánh giá Hoàng Nam": domain.FromMoves(
			facts(domain.Move{Kind: "completed", By: "system"}), at)[1],
		"Xe đạp Asama giao không thành":                          domain.FromMoves(facts(domain.Move{Kind: "failed", By: "system"}), at)[0],
		"Xe đạp Asama đã gửi cho người khác, đơn của bạn đã huỷ": cancelled("shipped_to_another"),
		"Người bán chưa gửi Xe đạp Asama, đơn của bạn đã huỷ":    cancelled("seller_timeout"),
		"Hãng vận chuyển chưa lấy Xe đạp Asama, đơn đã huỷ":      cancelled("pickup_timeout"),
		"Đức Huy trả 1,2 tr cho Xe đạp Asama":                    offer,
		"Hoàng Nam đã gửi Xe đạp Asama · GHN 8A2K9": domain.FromMoves(
			facts(domain.Move{Kind: "shipped", By: "seller", OrderID: order, Carrier: "ghn", TrackingCode: "8A2K9"}), at)[0],
		"Đức Huy trả 350.000 ₫ cho Xe đạp Asama":   domain.FromMoves(facts(domain.Move{Kind: "offer", By: "buyer", Amount: 350_000}), at)[0],
		"Tài khoản của bạn bị khoá đến 3/11":       domain.Suspended(buyer, "2026-11-03T08:00:00Z", at),
		"Tài khoản của bạn bị khoá":                domain.Suspended(buyer, "", at),
		"Đức Huy chấm 5 sao cho Xe đạp Asama":      domain.Reviewed(seller, "Đức Huy", "Xe đạp Asama", 5, at),
		"Hoàng Nam vừa đăng tin mới: Xe đạp Asama": domain.FollowedListing(buyer, seller, "Hoàng Nam", uuid.NewV7(), "Xe đạp Asama", at),
	} {
		if got := domain.Text(n.Type, n.Params); got != want {
			t.Errorf("%s: %q", want, got)
		}
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

func TestWatchAndPriceTexts(t *testing.T) {
	t.Parallel()
	watch, listing := uuid.NewV7(), uuid.NewV7()
	w := domain.WatchMatched(uuid.NewV7(), watch, "xe đạp", 3, time.Now())
	if got := domain.Text(w.Type, w.Params); got != "Có 3 tin mới cho “xe đạp”" {
		t.Fatal(got)
	}
	if w.CollapseKey != "watch:"+watch.String() || domain.TargetOf(w.Type, w.Params) != (domain.Target{Kind: "search_watch", ID: watch.String()}) {
		t.Fatalf("%+v", w)
	}
	if m := domain.WatchMatched(uuid.NewV7(), watch, "xe đạp", 99, time.Now()); domain.Text(m.Type, m.Params) != "Có 99+ tin mới cho “xe đạp”" {
		t.Fatal(domain.Text(m.Type, m.Params))
	}
	p := domain.PriceDropped(uuid.NewV7(), listing, "Xe đạp Asama", 1_400_000, 1_500_000, time.Now())
	if got := domain.Text(p.Type, p.Params); got != "Xe đạp Asama giảm còn 1,4 tr, lúc lưu 1,5 tr" {
		t.Fatal(got)
	}
	if p.CollapseKey != "saved:"+listing.String() || domain.TargetOf(p.Type, p.Params).Kind != "listing" || domain.Title(p.Type) != "Đã lưu" {
		t.Fatalf("%+v", p)
	}
}

// The system's moves on an order tell both sides: the buyer, and the seller,
// whose listing goes back on sale or is sold without a move of theirs.
func TestSystemMovesTellTheSellerToo(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		move              domain.Move
		reason            string
		typ               domain.Type
		toSeller, toBuyer string
	}{
		"completed by the system": {
			domain.Move{Kind: "completed", By: "system"},
			"", domain.TypeCompleted,
			"Đức Huy đã nhận Xe đạp Asama", "Xe đạp Asama đã giao. Đánh giá Hoàng Nam",
		},
		"the seller's timeout": {
			domain.Move{Kind: "order_cancelled", By: "system"},
			"seller_timeout", domain.TypeOrderCancelled,
			"Bạn chưa tạo đơn Xe đạp Asama, đơn đã huỷ", "Người bán chưa gửi Xe đạp Asama, đơn của bạn đã huỷ",
		},
		"the pickup timeout": {
			domain.Move{Kind: "order_cancelled", By: "system"},
			"pickup_timeout", domain.TypeOrderCancelled,
			"Hãng vận chuyển chưa lấy Xe đạp Asama, đơn đã huỷ", "Hãng vận chuyển chưa lấy Xe đạp Asama, đơn đã huỷ",
		},
	} {
		c.move.OrderID, c.move.CancelReason = order, c.reason
		f := facts(c.move)
		got := domain.FromMoves(f, at)
		if len(got) != 2 || got[0].Recipient != seller || got[1].Recipient != buyer || got[0].Type != c.typ || got[1].Type != c.typ {
			t.Errorf("%s: %+v", name, got)
			continue
		}
		if s, b := domain.Text(got[0].Type, got[0].Params), domain.Text(got[1].Type, got[1].Params); s != c.toSeller || b != c.toBuyer {
			t.Errorf("%s: seller %q, buyer %q", name, s, b)
		}
	}
	// A cancel the listing caused tells the buyer only, as before.
	f := facts(domain.Move{Kind: "order_cancelled", By: "system", OrderID: order, CancelReason: "shipped_to_another"})
	if got := domain.FromMoves(f, at); len(got) != 1 || got[0].Recipient != buyer {
		t.Errorf("shipped to another: %+v", got)
	}
}
