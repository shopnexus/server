package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

// dealView is the Deal schema, the latest order included.
type dealView struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Status         string `json:"status"`
	AgreedPrice    int64  `json:"agreed_price"`
	OpenPrice      *struct {
		By     string `json:"by"`
		Amount int64  `json:"amount"`
	} `json:"open_price"`
	OfferRange *struct {
		Min int64 `json:"min"`
		Max int64 `json:"max"`
	} `json:"offer_range"`
	Moves []struct {
		Kind   string `json:"kind"`
		By     string `json:"by"`
		Amount int64  `json:"amount"`
	} `json:"moves"`
	Order *struct {
		ID           string `json:"id"`
		Code         string `json:"code"`
		Status       string `json:"status"`
		CancelReason string `json:"cancel_reason"`
		Recipient    struct {
			Name   string `json:"name"`
			Phone  string `json:"phone"`
			Street string `json:"street"`
			Kind   string `json:"kind"`
		} `json:"recipient"`
		Shipment *struct {
			Mode         string `json:"mode"`
			Carrier      string `json:"carrier"`
			TrackingCode string `json:"tracking_code"`
			Fee          *int64 `json:"fee"`
			Status       string `json:"status"`
			DeliveredAt  string `json:"delivered_at"`
			Pickup       *struct {
				PointID string `json:"point_id"`
				Street  string `json:"street"`
			} `json:"pickup"`
		} `json:"shipment"`
	} `json:"order"`
}

// openDeal opens token's deal on listing and checks the status.
func openDeal(t *testing.T, token, listing string, status int) dealView {
	t.Helper()
	return expect[dealView](t, call(t, "POST", "/v1/deals", map[string]any{"listing_id": listing}, bearer(token)), status)
}

// dealCall posts to /v1/deals/{id}/{action} as token, signed out when it is
// empty.
func dealCall(t *testing.T, token, id, action string, body any) Response {
	t.Helper()
	return call(t, "POST", "/v1/deals/"+id+"/"+action, body, bearer(token))
}

// moved posts a move that must succeed and returns the deal.
func moved(t *testing.T, token, id, action string, body any) dealView {
	t.Helper()
	return expect[dealView](t, dealCall(t, token, id, action, body), http.StatusOK)
}

func getDeal(t *testing.T, token, id string) dealView {
	t.Helper()
	return expect[dealView](t, call(t, "GET", "/v1/deals/"+id, nil, bearer(token)), http.StatusOK)
}

func TestDealToAgreement(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)

	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	if d.Role != "buyer" || d.OfferRange == nil || d.OfferRange.Min != 750_000 || d.OfferRange.Max != 1_499_000 {
		t.Fatalf("%+v", d)
	}
	if again := openDeal(t, buyer.AccessToken, l.ID, http.StatusOK); again.ID != d.ID {
		t.Fatalf("a second deal: %s", again.ID)
	}
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})
	if s := getDeal(t, seller.AccessToken, d.ID); s.Role != "seller" || s.OpenPrice == nil || s.OpenPrice.By != "buyer" {
		t.Fatalf("seller reads: %+v", s)
	}
	moved(t, seller.AccessToken, d.ID, "offer", map[string]any{"amount": 1_300_000})
	agreed := moved(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_300_000})
	if agreed.Status != "agreed" || agreed.AgreedPrice != 1_300_000 {
		t.Fatalf("%+v", agreed)
	}
	for _, c := range []struct{ token, role string }{{buyer.AccessToken, "buying"}, {seller.AccessToken, "selling"}} {
		list := expect[page[dealView]](t, call(t, "GET", "/v1/me/deals?role="+c.role, nil, bearer(c.token)), http.StatusOK)
		if len(list.Items) != 1 || list.Items[0].ID != d.ID {
			t.Fatalf("%s: %+v", c.role, list)
		}
	}
	expectStatus(t, call(t, "GET", "/v1/me/deals?role=bogus", nil, bearer(buyer.AccessToken)), http.StatusBadRequest)
	if c := moved(t, seller.AccessToken, d.ID, "cancel", nil); c.Status != "negotiating" {
		t.Fatalf("%+v", c)
	}
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})
	moved(t, seller.AccessToken, d.ID, "decline", nil)
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})
	if w := moved(t, buyer.AccessToken, d.ID, "withdraw", nil); w.OpenPrice != nil {
		t.Fatalf("%+v", w)
	}
}

func TestDealFollowsTheListing(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})

	body := draftWith(l.Photos[0].UploadID)
	body["price"] = 1_400_000
	expectStatus(t, call(t, "PUT", "/v1/listings/"+l.ID, body, bearer(seller.AccessToken), newKey()), http.StatusOK)
	var got dealView
	testenv.Eventually(t, 10*time.Second, func() error {
		res := call(t, "GET", "/v1/deals/"+d.ID, nil, bearer(buyer.AccessToken))
		if got = expect[dealView](t, res, http.StatusOK); got.OpenPrice != nil || got.Moves[len(got.Moves)-1].Kind != "price_changed" {
			return fmt.Errorf("the deal did not follow the listing: %s", res.Body)
		}
		return nil
	})
	if last := got.Moves[len(got.Moves)-1]; last.By != "system" || last.Amount != 1_400_000 {
		t.Fatalf("%+v", last)
	}
}

func TestDealRefusals(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	open := func(token string, listing any) Response {
		t.Helper()
		return call(t, "POST", "/v1/deals", map[string]any{"listing_id": listing}, bearer(token))
	}

	check(t, "open signed out", open("", l.ID), 401, "unauthenticated")
	check(t, "open a bad id", open(buyer.AccessToken, "x"), 400, "")
	check(t, "open an unknown listing", open(buyer.AccessToken, uuid.NewV7().String()), 404, "listing_not_found")
	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "open suspended", open(suspended.AccessToken, l.ID), 403, "account_suspended")
	check(t, "open own listing", open(seller.AccessToken, l.ID), 409, "own_listing")
	check(t, "open a hidden listing", open(buyer.AccessToken, activeHidden(t, seller.AccessToken)), 409, "listing_unavailable")

	check(t, "get signed out", call(t, "GET", "/v1/deals/"+d.ID, nil), 401, "unauthenticated")
	check(t, "get a bad id", call(t, "GET", "/v1/deals/not-a-uuid", nil, bearer(buyer.AccessToken)), 400, "")
	check(t, "get as a stranger", call(t, "GET", "/v1/deals/"+d.ID, nil, bearer(stranger.AccessToken)), 404, "deal_not_found")
	check(t, "list signed out", call(t, "GET", "/v1/me/deals?role=buying", nil), 401, "unauthenticated")

	check(t, "offer too low", dealCall(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 500_000}), 422, "invalid_amount")
	check(t, "seller offers first", dealCall(t, seller.AccessToken, d.ID, "offer", map[string]any{"amount": 1_200_000}), 409, "move_not_allowed")
	check(t, "offer as a stranger", dealCall(t, stranger.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000}), 404, "deal_not_found")
	check(t, "offer signed out", dealCall(t, "", d.ID, "offer", map[string]any{"amount": 1_000_000}), 401, "unauthenticated")
	check(t, "offer a bad amount", dealCall(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": "x"}), 400, "")
	check(t, "accept a changed price", dealCall(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_400_000}), 409, "price_changed")
	check(t, "accept as a stranger", dealCall(t, stranger.AccessToken, d.ID, "accept", map[string]any{"price": 1_500_000}), 404, "deal_not_found")
	check(t, "accept signed out", dealCall(t, "", d.ID, "accept", map[string]any{"price": 1_500_000}), 401, "unauthenticated")
	check(t, "accept a bad id", dealCall(t, buyer.AccessToken, "not-a-uuid", "accept", map[string]any{"price": 1_500_000}), 400, "")

	late, _ := newAccount(t)
	ld := openDeal(t, late.AccessToken, l.ID, http.StatusCreated)
	suspend(t, late.Account.ID)
	check(t, "offer suspended", dealCall(t, late.AccessToken, ld.ID, "offer", map[string]any{"amount": 1_000_000}), 403, "account_suspended")
	check(t, "accept suspended", dealCall(t, late.AccessToken, ld.ID, "accept", map[string]any{"price": 1_500_000}), 403, "account_suspended")

	for _, action := range []string{"decline", "withdraw", "cancel"} {
		check(t, action+" with nothing to take back", dealCall(t, buyer.AccessToken, d.ID, action, nil), 409, "move_not_allowed")
		check(t, action+" as a stranger", dealCall(t, stranger.AccessToken, d.ID, action, nil), 404, "deal_not_found")
		check(t, action+" signed out", dealCall(t, "", d.ID, action, nil), 401, "unauthenticated")
		check(t, action+" a bad id", dealCall(t, buyer.AccessToken, "not-a-uuid", action, nil), 400, "")
	}
}
