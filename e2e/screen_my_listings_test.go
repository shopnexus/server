package e2e

import (
	"net/http"
	"testing"
	"time"
)

// myListingsItem is the MyListingsItem schema.
type myListingsItem struct {
	Listing struct {
		ID     string     `json:"id"`
		Status string     `json:"status"`
		SoldAt *time.Time `json:"sold_at"`
	} `json:"listing"`
	Views   int `json:"views"`
	Views7d int `json:"views_7d"`
	Askers  int `json:"askers"`
	Saves   int `json:"saves"`
	Order   *struct {
		DealID  string `json:"deal_id"`
		OrderID string `json:"order_id"`
		Status  string `json:"status"`
	} `json:"order"`
}

// myListingsScreen is the MyListingsScreen schema.
type myListingsScreen struct {
	Week struct {
		Views  int `json:"views"`
		Askers int `json:"askers"`
		Saves  int `json:"saves"`
	} `json:"week"`
	Items      []myListingsItem `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

func myListings(t *testing.T, token, query string) myListingsScreen {
	t.Helper()
	return expect[myListingsScreen](t, call(t, "GET", "/v1/screens/my-listings?"+query, nil, bearer(token)), http.StatusOK)
}

// Tin của bạn: the listings on sale with their views, askers, saves and
// order, the sold ones apart, and the week over the listings on sale.
func TestMyListingsScreen(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	ann, _ := newAccount(t)
	bao, _ := newAccount(t)
	token := seller.AccessToken
	sold := activeListing(t, token, nil).ID
	expectStatus(t, call(t, "POST", "/v1/listings/"+sold+"/mark-sold", nil, bearer(token)), http.StatusOK)
	quiet := activeListing(t, token, nil).ID
	busy := activeListing(t, token, nil).ID

	// Ann opens busy twice and asks for COD; Bao opens it, writes about it
	// and saves it; the seller and a guest open it without a count.
	for _, who := range []string{ann.AccessToken, ann.AccessToken, bao.AccessToken, token, ""} {
		expectStatus(t, call(t, "GET", "/v1/screens/listing/"+busy, nil, bearer(who)), http.StatusOK)
	}
	order := pendingOrder(t, ann.AccessToken, busy)
	openChat(t, bao.AccessToken, busy, "Còn hàng không?")
	expectStatus(t, call(t, "PUT", "/v1/saved/"+busy, nil, bearer(bao.AccessToken)), http.StatusNoContent)

	v := myListings(t, token, "pane=on_sale")
	if len(v.Items) != 2 || v.Items[0].Listing.ID != busy || v.Items[1].Listing.ID != quiet || v.NextCursor != nil {
		t.Fatalf("on sale: %+v", v)
	}
	got := v.Items[0]
	if got.Views != 2 || got.Views7d != 2 || got.Askers != 2 || got.Saves != 1 || got.Listing.Status != "active" {
		t.Fatalf("busy: %+v", got)
	}
	if o := got.Order; o == nil || o.DealID != order.ID || o.OrderID != order.Order.ID || o.Status != "pending" {
		t.Fatalf("busy's order: %+v", o)
	}
	if q := v.Items[1]; q.Views != 0 || q.Askers != 0 || q.Saves != 0 || q.Order != nil {
		t.Fatalf("quiet: %+v", q)
	}
	if v.Week.Views != 2 || v.Week.Askers != 2 || v.Week.Saves != 1 {
		t.Fatalf("week: %+v", v.Week)
	}

	first := myListings(t, token, "pane=on_sale&limit=1")
	if len(first.Items) != 1 || first.Items[0].Listing.ID != busy || first.NextCursor == nil || first.Week != v.Week {
		t.Fatalf("first page of one: %+v", first)
	}
	if rest := myListings(t, token, "pane=on_sale&limit=1&cursor="+*first.NextCursor); len(rest.Items) != 1 ||
		rest.Items[0].Listing.ID != quiet || rest.NextCursor != nil {
		t.Fatalf("second page of one: %+v", rest)
	}
	s := myListings(t, token, "pane=sold")
	if len(s.Items) != 1 || s.Items[0].Listing.ID != sold || s.Items[0].Listing.SoldAt == nil || s.Week != v.Week {
		t.Fatalf("sold: %+v", s)
	}

	check(t, "unknown pane", call(t, "GET", "/v1/screens/my-listings?pane=everything", nil, bearer(token)), http.StatusBadRequest, "")
	check(t, "no pane", call(t, "GET", "/v1/screens/my-listings", nil, bearer(token)), http.StatusBadRequest, "")
	check(t, "signed out", call(t, "GET", "/v1/screens/my-listings?pane=sold", nil), http.StatusUnauthorized, "")
}

// photoCopies is the ListingPhotoCopies schema.
type photoCopies struct {
	Items []struct {
		UploadID string `json:"upload_id"`
		URL      string `json:"url"`
	} `json:"items"`
}

// Đăng lại: the photos of a sold listing become new uploads that a new
// listing takes, while the sold listing keeps its own. A repeated key
// returns the same uploads, and a new key makes new ones.
func TestCopyListingPhotos(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	other, _ := newAccount(t)
	token := seller.AccessToken
	old := activeListing(t, token, nil)
	expectStatus(t, call(t, "POST", "/v1/listings/"+old.ID+"/mark-sold", nil, bearer(token)), http.StatusOK)
	path := "/v1/listings/" + old.ID + "/photo-copies"

	key := newKey()
	c := expect[photoCopies](t, call(t, "POST", path, nil, bearer(token), key), http.StatusCreated)
	if len(c.Items) != 1 || c.Items[0].UploadID == old.Photos[0].UploadID || c.Items[0].URL == "" {
		t.Fatalf("copies: %+v", c)
	}
	if again := expect[photoCopies](t, call(t, "POST", path, nil, bearer(token), key), http.StatusCreated); len(again.Items) != 1 ||
		again.Items[0].UploadID != c.Items[0].UploadID {
		t.Fatalf("the same key: %+v, first %+v", again, c)
	}
	if fresh := expect[photoCopies](t, call(t, "POST", path, nil, bearer(token), newKey()), http.StatusCreated); len(fresh.Items) != 1 ||
		fresh.Items[0].UploadID == c.Items[0].UploadID {
		t.Fatalf("a new key: %+v, first %+v", fresh, c)
	}
	res, err := http.Get(c.Items[0].URL) //nolint:noctx // A test reads a presigned URL.
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the copy reads %d", res.StatusCode)
	}
	posted := expect[listing](t, call(t, "POST", "/v1/listings", draftWith(c.Items[0].UploadID), bearer(token), newKey()), http.StatusCreated)
	if l := settled(t, token, posted.ID); l.Status != "active" || len(l.Photos) != 1 || l.Photos[0].URL == "" {
		t.Fatalf("the new listing: %+v", l)
	}
	if kept := expect[listing](t, call(t, "GET", "/v1/listings/"+old.ID, nil, bearer(token)), http.StatusOK); kept.Photos[0].URL != old.Photos[0].URL {
		t.Fatalf("the sold listing's photo: %+v", kept.Photos)
	}

	check(t, "signed out", call(t, "POST", path, nil, newKey()), http.StatusUnauthorized, "")
	check(t, "bad id", call(t, "POST", "/v1/listings/not-a-uuid/photo-copies", nil, bearer(token), newKey()), http.StatusBadRequest, "")
	check(t, "no key", call(t, "POST", path, nil, bearer(token)), http.StatusBadRequest, "")
	check(t, "another seller's", call(t, "POST", path, nil, bearer(other.AccessToken), newKey()), http.StatusNotFound, "listing_not_found")
	expectStatus(t, call(t, "DELETE", "/v1/listings/"+old.ID, nil, bearer(token)), http.StatusNoContent)
	check(t, "a deleted listing", call(t, "POST", path, nil, bearer(token), newKey()), http.StatusNotFound, "listing_not_found")
}
