package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
)

type addressView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Phone        string `json:"phone"`
	Street       string `json:"street"`
	CommuneCode  string `json:"commune_code"`
	ProvinceCode string `json:"province_code"`
	Commune      string `json:"commune"`
	Province     string `json:"province"`
	Kind         string `json:"kind"`
	IsDefault    bool   `json:"is_default"`
}

func addressBody() map[string]any {
	return map[string]any{"name": "Hân", "phone": "0912345678", "street": "12 Lý Thường Kiệt", "commune_code": "00004", "kind": "home"}
}

// newAddress adds an address to token's book.
func newAddress(t *testing.T, token string) addressView {
	t.Helper()
	return expect[addressView](t, call(t, "POST", "/v1/addresses", addressBody(), bearer(token), newKey()), http.StatusCreated)
}

func addresses(t *testing.T, token string) []addressView {
	t.Helper()
	return expect[page[addressView]](t, call(t, "GET", "/v1/addresses", nil, bearer(token)), http.StatusOK).Items
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
	if !first.IsDefault || first.Phone != "+84912345678" || first.Commune == "" || first.Province == "" || first.ProvinceCode != "01" || first.Kind != "home" {
		t.Fatalf("%+v", first)
	}
	second := newAddress(t, me.AccessToken)
	if second.IsDefault {
		t.Fatalf("a second address is not the default: %+v", second)
	}

	res := call(t, "PUT", "/v1/addresses/"+second.ID,
		map[string]any{"name": " Minh ", "phone": "+84 98 765 4321", "street": "1 Lê Lợi", "commune_code": "00004", "kind": "work"},
		bearer(me.AccessToken))
	if got := decode[addressView](t, res); res.Status != http.StatusOK || got.Name != "Minh" || got.Phone != "+84987654321" ||
		got.Kind != "work" || got.IsDefault {
		t.Fatalf("update: %d %s", res.Status, res.Body)
	}
	expectStatus(t, call(t, "POST", "/v1/addresses/"+second.ID+"/default", nil, bearer(me.AccessToken)), http.StatusNoContent)
	third := newAddress(t, me.AccessToken)
	book := addresses(t, me.AccessToken)
	if len(book) != 3 || book[0].ID != second.ID || !book[0].IsDefault || book[1].ID != third.ID || book[2].ID != first.ID || book[2].IsDefault {
		t.Fatalf("the default first, then the newest: %+v", book)
	}

	expectStatus(t, call(t, "DELETE", "/v1/addresses/"+second.ID, nil, bearer(me.AccessToken)), http.StatusNoContent)
	if book = addresses(t, me.AccessToken); len(book) != 2 || book[0].ID != third.ID || !book[0].IsDefault {
		t.Fatalf("the newest remaining is the default: %+v", book)
	}
}

// is_default true makes an added or edited address the default in the same
// request; false or absent leaves the default where it is. A retried add
// returns its address as it is now and moves nothing.
func TestAddressDefaultInTheSameRequest(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	auth := bearer(me.AccessToken)
	notDefault := addressBody()
	notDefault["is_default"] = false
	first := expect[addressView](t, call(t, "POST", "/v1/addresses", notDefault, auth, newKey()), http.StatusCreated)
	if !first.IsDefault {
		t.Fatalf("the first address is the default whatever is_default says: %+v", first)
	}
	asDefault := addressBody()
	asDefault["is_default"] = true
	key := newKey()
	second := expect[addressView](t, call(t, "POST", "/v1/addresses", asDefault, auth, key), http.StatusCreated)
	if book := addresses(t, me.AccessToken); !second.IsDefault || len(book) != 2 || book[0].ID != second.ID || book[1].IsDefault {
		t.Fatalf("added as the default: %+v, book %+v", second, book)
	}

	edited := expect[addressView](t, call(t, "PUT", "/v1/addresses/"+first.ID, asDefault, auth), http.StatusOK)
	if book := addresses(t, me.AccessToken); !edited.IsDefault || book[0].ID != first.ID || book[1].IsDefault {
		t.Fatalf("edited as the default: %+v, book %+v", edited, book)
	}
	for name, body := range map[string]map[string]any{"is_default false": notDefault, "no is_default": addressBody()} {
		if got := expect[addressView](t, call(t, "PUT", "/v1/addresses/"+first.ID, body, auth), http.StatusOK); !got.IsDefault {
			t.Errorf("the default edited with %s: %+v", name, got)
		}
		if got := expect[addressView](t, call(t, "PUT", "/v1/addresses/"+second.ID, body, auth), http.StatusOK); got.IsDefault {
			t.Errorf("another edited with %s: %+v", name, got)
		}
	}

	again := expect[addressView](t, call(t, "POST", "/v1/addresses", asDefault, auth, key), http.StatusCreated)
	if book := addresses(t, me.AccessToken); again.ID != second.ID || again.IsDefault || len(book) != 2 || book[0].ID != first.ID {
		t.Fatalf("a retried add moved the default: %+v, book %+v", again, book)
	}
	check(t, "is_default not a boolean", call(t, "POST", "/v1/addresses",
		map[string]any{"name": "Hân", "phone": "0912345678", "street": "12 Lý", "commune_code": "00004", "kind": "home", "is_default": "yes"},
		auth, newKey()), 400, "invalid_request")
}

func TestAddressRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	stranger, _ := newAccount(t)
	mine := newAddress(t, me.AccessToken)
	path := "/v1/addresses/" + mine.ID

	check(t, "list signed out", call(t, "GET", "/v1/addresses", nil), 401, "unauthenticated")
	check(t, "add signed out", call(t, "POST", "/v1/addresses", addressBody(), newKey()), 401, "unauthenticated")
	check(t, "add without a key", call(t, "POST", "/v1/addresses", addressBody(), bearer(me.AccessToken)), 400, "")
	key := newKey()
	first := call(t, "POST", "/v1/addresses", addressBody(), bearer(me.AccessToken), key)
	again := call(t, "POST", "/v1/addresses", addressBody(), bearer(me.AccessToken), key)
	if first.Status != 201 || again.Status != 201 || decode[addressView](t, again).ID != decode[addressView](t, first).ID {
		t.Errorf("a retried add: %d %s, %d %s", first.Status, first.Body, again.Status, again.Body)
	}
	check(t, "update signed out", call(t, "PUT", path, addressBody()), 401, "unauthenticated")
	check(t, "set default signed out", call(t, "POST", path+"/default", nil), 401, "unauthenticated")
	check(t, "delete signed out", call(t, "DELETE", path, nil), 401, "unauthenticated")

	extra := addressBody()
	extra["note"] = "gọi trước"
	check(t, "add with an unknown field", call(t, "POST", "/v1/addresses", extra, bearer(me.AccessToken), newKey()), 400, "invalid_request")
	check(t, "update a bad id", call(t, "PUT", "/v1/addresses/not-a-uuid", addressBody(), bearer(me.AccessToken)), 400, "")
	check(t, "set default a bad id", call(t, "POST", "/v1/addresses/not-a-uuid/default", nil, bearer(me.AccessToken)), 400, "")
	check(t, "delete a bad id", call(t, "DELETE", "/v1/addresses/not-a-uuid", nil, bearer(me.AccessToken)), 400, "")

	bad := map[string]any{"name": " ", "phone": "123", "street": "", "commune_code": "nope", "kind": "office"}
	res := call(t, "POST", "/v1/addresses", bad, bearer(me.AccessToken), newKey())
	if e := errorOf(t, res); res.Status != 422 || e.Code != "invalid_address" || len(e.Fields) != 5 {
		t.Errorf("add a bad address: %d %s", res.Status, res.Body)
	}
	check(t, "update to a bad phone", call(t, "PUT", path,
		map[string]any{"name": "Hân", "phone": "123", "street": "12 Lý", "commune_code": "00004", "kind": "home"},
		bearer(me.AccessToken)), 422, "invalid_address")
	noKind := addressBody()
	delete(noKind, "kind")
	check(t, "add without a kind", call(t, "POST", "/v1/addresses", noKind, bearer(me.AccessToken), newKey()), 400, "invalid_request")
	for _, kind := range []string{"office", "", "Home"} {
		body := addressBody()
		body["kind"] = kind
		res = call(t, "POST", "/v1/addresses", body, bearer(me.AccessToken), newKey())
		if e := errorOf(t, res); res.Status != 422 || e.Code != "invalid_address" || len(e.Fields) != 1 || e.Fields["kind"] == "" ||
			e.FieldCodes["kind"] != "not_allowed" {
			t.Errorf("add kind %q: %d %s", kind, res.Status, res.Body)
		}
		res = call(t, "PUT", path, body, bearer(me.AccessToken))
		if e := errorOf(t, res); res.Status != 422 || e.Fields["kind"] == "" {
			t.Errorf("update to kind %q: %d %s", kind, res.Status, res.Body)
		}
	}

	check(t, "update someone else's", call(t, "PUT", path, addressBody(), bearer(stranger.AccessToken)), 404, "address_not_found")
	check(t, "set default someone else's", call(t, "POST", path+"/default", nil, bearer(stranger.AccessToken)), 404, "address_not_found")
	check(t, "delete someone else's", call(t, "DELETE", path, nil, bearer(stranger.AccessToken)), 404, "address_not_found")
	if book := addresses(t, stranger.AccessToken); len(book) != 0 {
		t.Errorf("someone else's book: %+v", book)
	}

	for range 8 {
		newAddress(t, me.AccessToken)
	}
	check(t, "the eleventh", call(t, "POST", "/v1/addresses", addressBody(), bearer(me.AccessToken), newKey()), 409, "address_limit_reached")
	check(t, "delete", call(t, "DELETE", path, nil, bearer(me.AccessToken)), 204, "")
	check(t, "delete again", call(t, "DELETE", path, nil, bearer(me.AccessToken)), 404, "address_not_found")
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
			res := call(t, "PUT", path, body, bearer(me.AccessToken))
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

// Each kind of address is kept and read back as it was sent.
func TestAddressKinds(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	for _, kind := range []string{"home", "work", "other"} {
		body := addressBody()
		body["kind"] = kind
		made := expect[addressView](t, call(t, "POST", "/v1/addresses", body, bearer(me.AccessToken), newKey()), http.StatusCreated)
		if made.Kind != kind {
			t.Errorf("add %s: %+v", kind, made)
		}
	}
	book := addresses(t, me.AccessToken)
	kinds := map[string]bool{}
	for _, a := range book {
		kinds[a.Kind] = true
	}
	if len(book) != 3 || !kinds["home"] || !kinds["work"] || !kinds["other"] {
		t.Fatalf("the book: %+v", book)
	}
}

// The order keeps a copy of the address it was requested to, its kind
// included.
func TestOrderKeepsItsRecipient(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	work := addressBody()
	work["kind"] = "work"
	address := expect[addressView](t, call(t, "POST", "/v1/addresses", work, bearer(buyer.AccessToken), newKey()), http.StatusCreated)
	d := openDeal(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID, http.StatusCreated)
	moved(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_500_000})
	moved(t, buyer.AccessToken, d.ID, "cod", codBody(address.ID))
	res := call(t, "PUT", "/v1/addresses/"+address.ID,
		map[string]any{"name": "Minh", "phone": "0987654321", "street": "1 Lê Lợi", "commune_code": "00004", "kind": "home"},
		bearer(buyer.AccessToken))
	expectStatus(t, res, http.StatusOK)
	expectStatus(t, call(t, "DELETE", "/v1/addresses/"+address.ID, nil, bearer(buyer.AccessToken)), http.StatusNoContent)
	for _, token := range []string{buyer.AccessToken, seller.AccessToken} {
		r := getDeal(t, token, d.ID).Order.Recipient
		if r.Name != "Hân" || r.Phone != "+84912345678" || r.Street != "12 Lý Thường Kiệt" || r.Kind != "work" {
			t.Fatalf("%+v", r)
		}
	}
}
