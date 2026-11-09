package e2e

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"uuid"
)

func TestSearchByPhoto(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	mine := upload(t, me.AccessToken, photoWithGPS(t))
	theirs := upload(t, other.AccessToken, photoWithGPS(t))

	res, _ := search(t, url.Values{"photo": {mine}})
	check(t, "signed out", res, http.StatusUnauthorized, "unauthenticated")
	res, _ = search(t, url.Values{"photo": {mine}, "q": {"ghế"}}, bearer(me.AccessToken))
	check(t, "with q", res, http.StatusBadRequest, "search_conflict")
	res, _ = search(t, url.Values{"photo": {mine}, "like": {uuid.NewV7().String()}}, bearer(me.AccessToken))
	check(t, "with like", res, http.StatusBadRequest, "search_conflict")
	res, _ = search(t, url.Values{"photo": {theirs}}, bearer(me.AccessToken))
	check(t, "someone else's", res, http.StatusUnprocessableEntity, "invalid_upload")

	res, v := search(t, url.Values{"photo": {mine}}, bearer(me.AccessToken))
	if res.Status != http.StatusOK || v.Understood != "" || len(v.Chips) != 0 {
		t.Fatalf("a photo search: %d %s", res.Status, res.Body)
	}
	if res, _ = search(t, url.Values{"photo": {mine}, "cursor": {"20"}}, bearer(me.AccessToken)); res.Status != http.StatusOK {
		t.Fatalf("a later page reuses the cached vector: %d %s", res.Status, res.Body)
	}
}

func TestSearchByPhotoUnavailable(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	down := upload(t, me.AccessToken, magentaPhoto(t))
	res, _ := search(t, url.Values{"photo": {down}}, bearer(me.AccessToken))
	check(t, "the image alias answers 503", res, http.StatusServiceUnavailable, "search_unavailable")
}

func TestSearchLike(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	viewer, _ := newAccount(t)
	lo, hi := band()
	fields := func(title string, price int64) map[string]any {
		return map[string]any{"title": title, "category_code": "furniture.other", "price": price}
	}
	source := activeListing(t, seller.AccessToken, fields("Ghế gỗ sồi tự nhiên", lo))
	twin := activeListing(t, seller.AccessToken, fields("Ghế gỗ sồi tự nhiên cùng mẫu", hi))
	like := url.Values{
		"like": {source.ID}, "price_min": {strconv.FormatInt(lo, 10)}, "price_max": {strconv.FormatInt(hi, 10)},
	}

	res, v := search(t, like, bearer(viewer.AccessToken))
	if res.Status != http.StatusOK || len(v.Items) != 1 || v.Items[0].ID != twin.ID {
		t.Fatalf("the twin and not the source: %d %s", res.Status, res.Body)
	}
	if res, v = search(t, like); res.Status != http.StatusOK || len(v.Items) != 1 {
		t.Fatalf("signed out: %d %s", res.Status, res.Body)
	}
	if res, v = search(t, like, bearer(seller.AccessToken)); res.Status != http.StatusOK || len(v.Items) != 0 {
		t.Fatalf("the seller's own listings stay out: %d %s", res.Status, res.Body)
	}
	withQ := url.Values{"like": {source.ID}, "q": {"ghế gỗ"}, "price_min": like["price_min"], "price_max": like["price_max"]}
	if res, v = search(t, withQ); res.Status != http.StatusOK || len(v.Items) != 1 || v.Items[0].ID != twin.ID {
		t.Fatalf("like with q: %d %s", res.Status, res.Body)
	}
	res, _ = search(t, url.Values{"like": {uuid.NewV7().String()}})
	check(t, "an unknown listing", res, http.StatusNotFound, "listing_not_found")
	hidden := activeHidden(t, seller.AccessToken)
	res, _ = search(t, url.Values{"like": {hidden}})
	check(t, "a hidden listing", res, http.StatusNotFound, "listing_not_found")
}
