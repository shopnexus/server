package e2e

import (
	"net/http"
	"testing"
)

// A prefetched listing view counts nothing until the client reports the
// open, which counts once a day like any other.
func TestPrefetchedListingView(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	ann, _ := newAccount(t)
	id := activeListing(t, seller.AccessToken, nil).ID
	views := func() int {
		t.Helper()
		return myListings(t, seller.AccessToken, "pane=on_sale").Items[0].Views
	}

	prefetch := [2]string{"Sec-Purpose", "prefetch"}
	expectStatus(t, call(t, "GET", "/v1/screens/listing/"+id, nil, bearer(ann.AccessToken), prefetch), http.StatusOK)
	if n := views(); n != 0 {
		t.Fatalf("a prefetch counts %d", n)
	}
	path := "/v1/viewed/" + id
	check(t, "open", call(t, "PUT", path, nil, bearer(ann.AccessToken)), http.StatusNoContent, "")
	check(t, "open again", call(t, "PUT", path, nil, bearer(ann.AccessToken)), http.StatusNoContent, "")
	check(t, "the seller's open", call(t, "PUT", path, nil, bearer(seller.AccessToken)), http.StatusNoContent, "")
	if n := views(); n != 1 {
		t.Fatalf("views %d, want 1", n)
	}
	check(t, "a bad id", call(t, "PUT", "/v1/viewed/x", nil, bearer(ann.AccessToken)), http.StatusBadRequest, "")
	check(t, "signed out", call(t, "PUT", path, nil), http.StatusUnauthorized, "unauthenticated")
}
