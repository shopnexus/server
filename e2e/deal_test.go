package e2e

import (
	"net/http"
	"testing"
	"time"
	"uuid"
)

type dealBody struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	AgreedPrice int64  `json:"agreed_price"`
	OpenPrice   *struct {
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
}

type dealList struct {
	Items []dealBody `json:"items"`
}

// openDeal opens token's deal on listing and checks the status.
func openDeal(t *testing.T, token, listing string, status int) dealBody {
	t.Helper()
	res := call(t, server, "POST", "/v1/deals", map[string]any{"listing_id": listing}, bearer(token))
	if res.Status != status {
		t.Fatalf("open: %d %s", res.Status, res.Body)
	}
	return decode[dealBody](t, res)
}

// dealCall posts to /v1/deals/{id}/{action}, as token when it is set.
func dealCall(t *testing.T, token, id, action string, body any) Response {
	t.Helper()
	var headers [][2]string
	if token != "" {
		headers = append(headers, bearer(token))
	}
	return call(t, server, "POST", "/v1/deals/"+id+"/"+action, body, headers...)
}

// moved posts a move that must succeed and returns the deal.
func moved(t *testing.T, token, id, action string, body any) dealBody {
	t.Helper()
	res := dealCall(t, token, id, action, body)
	if res.Status != http.StatusOK {
		t.Fatalf("%s: %d %s", action, res.Status, res.Body)
	}
	return decode[dealBody](t, res)
}

func TestDealToAgreement(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken)

	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	if d.Role != "buyer" || d.OfferRange == nil || d.OfferRange.Min != 750_000 || d.OfferRange.Max != 1_499_000 {
		t.Fatalf("%+v", d)
	}
	if again := openDeal(t, buyer.AccessToken, l.ID, http.StatusOK); again.ID != d.ID {
		t.Fatalf("a second deal: %s", again.ID)
	}
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})
	res := call(t, server, "GET", "/v1/deals/"+d.ID, nil, bearer(seller.AccessToken))
	if s := decode[dealBody](t, res); res.Status != http.StatusOK || s.Role != "seller" || s.OpenPrice == nil ||
		s.OpenPrice.By != "buyer" {
		t.Fatalf("seller reads: %d %s", res.Status, res.Body)
	}
	moved(t, seller.AccessToken, d.ID, "offer", map[string]any{"amount": 1_300_000})
	agreed := moved(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_300_000})
	if agreed.Status != "agreed" || agreed.AgreedPrice != 1_300_000 {
		t.Fatalf("%+v", agreed)
	}
	for _, c := range []struct{ token, role string }{{buyer.AccessToken, "buying"}, {seller.AccessToken, "selling"}} {
		res = call(t, server, "GET", "/v1/me/deals?role="+c.role, nil, bearer(c.token))
		if list := decode[dealList](t, res); res.Status != http.StatusOK || len(list.Items) != 1 || list.Items[0].ID != d.ID {
			t.Fatalf("%s: %d %s", c.role, res.Status, res.Body)
		}
	}
	if res = call(t, server, "GET", "/v1/me/deals?role=bogus", nil, bearer(buyer.AccessToken)); res.Status != http.StatusBadRequest {
		t.Fatalf("a bad role: %d", res.Status)
	}
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
	l := activeListing(t, seller.AccessToken)
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})

	body := draftWith(l.Photos[0].UploadID)
	body["price"] = 1_400_000
	if res := call(t, server, "PUT", "/v1/listings/"+l.ID, body, bearer(seller.AccessToken), newKey()); res.Status != http.StatusOK {
		t.Fatalf("edit: %d %s", res.Status, res.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		res := call(t, server, "GET", "/v1/deals/"+d.ID, nil, bearer(buyer.AccessToken))
		got := decode[dealBody](t, res)
		if last := got.Moves[len(got.Moves)-1]; got.OpenPrice == nil && last.Kind == "price_changed" {
			if last.By != "system" || last.Amount != 1_400_000 {
				t.Fatalf("%+v", last)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the deal did not follow the listing: %s", res.Body)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestDealRefusals(t *testing.T) {
	t.Parallel()
	expect := func(name string, res Response, status int, code string) {
		t.Helper()
		if res.Status != status || (code != "" && errorOf(t, res).Code != code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken)
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	open := func(token string, listing any) Response {
		t.Helper()
		var headers [][2]string
		if token != "" {
			headers = append(headers, bearer(token))
		}
		return call(t, server, "POST", "/v1/deals", map[string]any{"listing_id": listing}, headers...)
	}

	expect("open signed out", open("", l.ID), 401, "unauthenticated")
	expect("open a bad id", open(buyer.AccessToken, "x"), 400, "")
	expect("open an unknown listing", open(buyer.AccessToken, uuid.NewV7().String()), 404, "listing_not_found")
	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	expect("open suspended", open(suspended.AccessToken, l.ID), 403, "account_suspended")
	expect("open without a phone", open(emailAccount(t).AccessToken, l.ID), 403, "phone_required")
	expect("open own listing", open(seller.AccessToken, l.ID), 409, "own_listing")
	hidden := activeListing(t, seller.AccessToken)
	if res := call(t, server, "POST", "/v1/listings/"+hidden.ID+"/hide", nil, bearer(seller.AccessToken)); res.Status != http.StatusOK {
		t.Fatalf("hide: %d %s", res.Status, res.Body)
	}
	expect("open a hidden listing", open(buyer.AccessToken, hidden.ID), 409, "listing_unavailable")

	expect("get signed out", call(t, server, "GET", "/v1/deals/"+d.ID, nil), 401, "unauthenticated")
	expect("get a bad id", call(t, server, "GET", "/v1/deals/not-a-uuid", nil, bearer(buyer.AccessToken)), 400, "")
	expect("get as a stranger", call(t, server, "GET", "/v1/deals/"+d.ID, nil, bearer(stranger.AccessToken)), 404, "deal_not_found")
	expect("list signed out", call(t, server, "GET", "/v1/me/deals?role=buying", nil), 401, "unauthenticated")

	expect("offer too low", dealCall(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 500_000}), 422, "invalid_amount")
	expect("seller offers first", dealCall(t, seller.AccessToken, d.ID, "offer", map[string]any{"amount": 1_200_000}), 409, "move_not_allowed")
	expect("offer as a stranger", dealCall(t, stranger.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000}), 404, "deal_not_found")
	expect("offer signed out", dealCall(t, "", d.ID, "offer", map[string]any{"amount": 1_000_000}), 401, "unauthenticated")
	expect("offer a bad amount", dealCall(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": "x"}), 400, "")
	expect("accept a changed price", dealCall(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_400_000}), 409, "price_changed")
	expect("accept as a stranger", dealCall(t, stranger.AccessToken, d.ID, "accept", map[string]any{"price": 1_500_000}), 404, "deal_not_found")
	expect("accept signed out", dealCall(t, "", d.ID, "accept", map[string]any{"price": 1_500_000}), 401, "unauthenticated")
	expect("accept a bad id", dealCall(t, buyer.AccessToken, "not-a-uuid", "accept", map[string]any{"price": 1_500_000}), 400, "")

	late, _ := newAccount(t)
	ld := openDeal(t, late.AccessToken, l.ID, http.StatusCreated)
	suspend(t, late.Account.ID)
	expect("offer suspended", dealCall(t, late.AccessToken, ld.ID, "offer", map[string]any{"amount": 1_000_000}), 403, "account_suspended")
	expect("accept suspended", dealCall(t, late.AccessToken, ld.ID, "accept", map[string]any{"price": 1_500_000}), 403, "account_suspended")

	for _, action := range []string{"decline", "withdraw", "cancel"} {
		expect(action+" with nothing to take back", dealCall(t, buyer.AccessToken, d.ID, action, nil), 409, "move_not_allowed")
		expect(action+" as a stranger", dealCall(t, stranger.AccessToken, d.ID, action, nil), 404, "deal_not_found")
		expect(action+" signed out", dealCall(t, "", d.ID, action, nil), 401, "unauthenticated")
		expect(action+" a bad id", dealCall(t, buyer.AccessToken, "not-a-uuid", action, nil), 400, "")
	}
}
