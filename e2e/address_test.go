package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
)

type addressView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Street      string `json:"street"`
	CommuneCode string `json:"commune_code"`
	Commune     string `json:"commune"`
	Province    string `json:"province"`
	IsDefault   bool   `json:"is_default"`
}

type addressList struct {
	Items []addressView `json:"items"`
}

func addressBody() map[string]any {
	return map[string]any{"name": "Hân", "phone": "0912345678", "street": "12 Lý Thường Kiệt", "commune_code": "00004"}
}

// newAddress adds an address to token's book.
func newAddress(t *testing.T, token string) addressView {
	t.Helper()
	res := call(t, server, "POST", "/v1/addresses", addressBody(), bearer(token), newKey())
	if res.Status != http.StatusCreated {
		t.Fatalf("add address: %d %s", res.Status, res.Body)
	}
	return decode[addressView](t, res)
}

func addresses(t *testing.T, token string) []addressView {
	t.Helper()
	res := call(t, server, "GET", "/v1/addresses", nil, bearer(token))
	if res.Status != http.StatusOK {
		t.Fatalf("addresses: %d %s", res.Status, res.Body)
	}
	return decode[addressList](t, res).Items
}

// deliveryAddress is the ID of token's default address, added when the book
// is empty.
func deliveryAddress(t *testing.T, token string) string {
	t.Helper()
	if book := addresses(t, token); len(book) > 0 {
		return book[0].ID
	}
	return newAddress(t, token).ID
}

func TestAddressBook(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	first := newAddress(t, me.AccessToken)
	if !first.IsDefault || first.Phone != "+84912345678" || first.Commune == "" || first.Province == "" {
		t.Fatalf("%+v", first)
	}
	second := newAddress(t, me.AccessToken)
	if second.IsDefault {
		t.Fatalf("a second address is not the default: %+v", second)
	}

	res := call(t, server, "PUT", "/v1/addresses/"+second.ID,
		map[string]any{"name": " Minh ", "phone": "+84 98 765 4321", "street": "1 Lê Lợi", "commune_code": "00004"}, bearer(me.AccessToken))
	if got := decode[addressView](t, res); res.Status != http.StatusOK || got.Name != "Minh" || got.Phone != "+84987654321" || got.IsDefault {
		t.Fatalf("update: %d %s", res.Status, res.Body)
	}
	if res = call(t, server, "POST", "/v1/addresses/"+second.ID+"/default", nil, bearer(me.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("set default: %d %s", res.Status, res.Body)
	}
	third := newAddress(t, me.AccessToken)
	book := addresses(t, me.AccessToken)
	if len(book) != 3 || book[0].ID != second.ID || !book[0].IsDefault || book[1].ID != third.ID || book[2].ID != first.ID || book[2].IsDefault {
		t.Fatalf("the default first, then the newest: %+v", book)
	}

	if res = call(t, server, "DELETE", "/v1/addresses/"+second.ID, nil, bearer(me.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.Status, res.Body)
	}
	if book = addresses(t, me.AccessToken); len(book) != 2 || book[0].ID != third.ID || !book[0].IsDefault {
		t.Fatalf("the newest remaining is the default: %+v", book)
	}
}

func TestAddressRefusals(t *testing.T) {
	t.Parallel()
	expect := func(name string, res Response, status int, code string) {
		t.Helper()
		if res.Status != status || (code != "" && errorOf(t, res).Code != code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	me, _ := newAccount(t)
	stranger, _ := newAccount(t)
	mine := newAddress(t, me.AccessToken)
	path := "/v1/addresses/" + mine.ID

	expect("list signed out", call(t, server, "GET", "/v1/addresses", nil), 401, "unauthenticated")
	expect("add signed out", call(t, server, "POST", "/v1/addresses", addressBody(), newKey()), 401, "unauthenticated")
	expect("add without a key", call(t, server, "POST", "/v1/addresses", addressBody(), bearer(me.AccessToken)), 400, "")
	key := newKey()
	first := call(t, server, "POST", "/v1/addresses", addressBody(), bearer(me.AccessToken), key)
	again := call(t, server, "POST", "/v1/addresses", addressBody(), bearer(me.AccessToken), key)
	if first.Status != 201 || again.Status != 201 || decode[addressView](t, again).ID != decode[addressView](t, first).ID {
		t.Errorf("a retried add: %d %s, %d %s", first.Status, first.Body, again.Status, again.Body)
	}
	expect("update signed out", call(t, server, "PUT", path, addressBody()), 401, "unauthenticated")
	expect("set default signed out", call(t, server, "POST", path+"/default", nil), 401, "unauthenticated")
	expect("delete signed out", call(t, server, "DELETE", path, nil), 401, "unauthenticated")

	extra := addressBody()
	extra["is_default"] = true
	expect("add with an unknown field", call(t, server, "POST", "/v1/addresses", extra, bearer(me.AccessToken), newKey()), 400, "invalid_request")
	expect("update a bad id", call(t, server, "PUT", "/v1/addresses/not-a-uuid", addressBody(), bearer(me.AccessToken)), 400, "")
	expect("set default a bad id", call(t, server, "POST", "/v1/addresses/not-a-uuid/default", nil, bearer(me.AccessToken)), 400, "")
	expect("delete a bad id", call(t, server, "DELETE", "/v1/addresses/not-a-uuid", nil, bearer(me.AccessToken)), 400, "")

	bad := map[string]any{"name": " ", "phone": "123", "street": "", "commune_code": "nope"}
	res := call(t, server, "POST", "/v1/addresses", bad, bearer(me.AccessToken), newKey())
	if e := errorOf(t, res); res.Status != 422 || e.Code != "invalid_address" || len(e.Fields) != 4 {
		t.Errorf("add a bad address: %d %s", res.Status, res.Body)
	}
	expect("update to a bad phone", call(t, server, "PUT", path,
		map[string]any{"name": "Hân", "phone": "123", "street": "12 Lý", "commune_code": "00004"}, bearer(me.AccessToken)), 422, "invalid_address")

	expect("update someone else's", call(t, server, "PUT", path, addressBody(), bearer(stranger.AccessToken)), 404, "address_not_found")
	expect("set default someone else's", call(t, server, "POST", path+"/default", nil, bearer(stranger.AccessToken)), 404, "address_not_found")
	expect("delete someone else's", call(t, server, "DELETE", path, nil, bearer(stranger.AccessToken)), 404, "address_not_found")
	if book := addresses(t, stranger.AccessToken); len(book) != 0 {
		t.Errorf("someone else's book: %+v", book)
	}

	for range 8 {
		newAddress(t, me.AccessToken)
	}
	expect("the eleventh", call(t, server, "POST", "/v1/addresses", addressBody(), bearer(me.AccessToken), newKey()), 409, "address_limit_reached")
	expect("delete", call(t, server, "DELETE", path, nil, bearer(me.AccessToken)), 204, "")
	expect("delete again", call(t, server, "DELETE", path, nil, bearer(me.AccessToken)), 404, "address_not_found")
}

// Parallel edits of one address race on its write_id. Each gets 200 or 409
// try_again, and at least one wins.
func TestAddressUpdateRace(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	path := "/v1/addresses/" + newAddress(t, me.AccessToken).ID
	statuses := make(chan int, 30)
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() {
			body := addressBody()
			body["street"] = fmt.Sprint(i, " Lý Thường Kiệt")
			res := call(t, server, "PUT", path, body, bearer(me.AccessToken))
			if res.Status == 409 && errorOf(t, res).Code != "try_again" {
				t.Errorf("409 with %s", res.Body)
			}
			statuses <- res.Status
		})
	}
	wg.Wait()
	close(statuses)
	won := 0
	for s := range statuses {
		if s != 200 && s != 409 {
			t.Fatalf("status %d", s)
		}
		if s == 200 {
			won++
		}
	}
	if won == 0 {
		t.Fatal("no edit won")
	}
}

// The order keeps a copy of the address it was requested to.
func TestOrderKeepsItsRecipient(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	address := newAddress(t, buyer.AccessToken)
	d := openDeal(t, buyer.AccessToken, activeListing(t, seller.AccessToken).ID, http.StatusCreated)
	moved(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_500_000})
	if res := dealCall(t, buyer.AccessToken, d.ID, "cod", map[string]any{"address_id": address.ID}); res.Status != http.StatusOK {
		t.Fatalf("cod: %d %s", res.Status, res.Body)
	}
	res := call(t, server, "PUT", "/v1/addresses/"+address.ID,
		map[string]any{"name": "Minh", "phone": "0987654321", "street": "1 Lê Lợi", "commune_code": "00004"}, bearer(buyer.AccessToken))
	if res.Status != http.StatusOK {
		t.Fatalf("update: %d %s", res.Status, res.Body)
	}
	if res = call(t, server, "DELETE", "/v1/addresses/"+address.ID, nil, bearer(buyer.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.Status, res.Body)
	}
	for _, token := range []string{buyer.AccessToken, seller.AccessToken} {
		r := getDeal(t, token, d.ID).Order.Recipient
		if r.Name != "Hân" || r.Phone != "+84912345678" || r.Street != "12 Lý Thường Kiệt" {
			t.Fatalf("%+v", r)
		}
	}
}
