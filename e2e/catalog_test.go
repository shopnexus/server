package e2e

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

type listing struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Price  int64  `json:"price"`
	Photos []struct {
		UploadID string `json:"upload_id"`
		URL      string `json:"url"`
		ThumbURL string `json:"thumb_url"`
		Copied   bool   `json:"copied"`
	} `json:"photos"`
	Location struct {
		Province struct {
			DisplayName string `json:"display_name"`
		} `json:"province"`
	} `json:"location"`
}

// photoWithGPS is a JPEG carrying an APP1 EXIF segment, as a phone writes
// with the GPS position.
func photoWithGPS(t *testing.T) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 80, 60))
	for y := range 60 {
		for x := range 80 {
			m.Set(x, y, color.RGBA{uint8(x * 3), uint8(y * 4), 120, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, nil); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("Exif\x00\x00"), []byte("II*\x00\x08\x00\x00\x00\x00\x00GPS 10.7769N 106.7009E")...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	jpg := b.Bytes()
	out := append(append(append([]byte{}, jpg[:2]...), seg...), payload...)
	return append(out, jpg[2:]...)
}

func hasExif(b []byte) bool { return bytes.Contains(b, []byte("Exif\x00\x00")) }

// draftWith is a valid listing body with the given photos.
func draftWith(photos ...string) map[string]any {
	return map[string]any{
		"title": "Xe đạp Asama", "price": 1_500_000, "description": "Đi ít", "category_code": "vehicles.bicycle",
		"condition": "used", "allow_offers": true, "commune_code": "00004", "photos": photos,
	}
}

// postListing posts a listing with one fresh photo as the account with
// token, with fields over the body draftWith gives.
func postListing(t *testing.T, token string, fields map[string]any) listing {
	t.Helper()
	body := draftWith(upload(t, token, photoWithGPS(t)))
	maps.Copy(body, fields)
	return expect[listing](t, call(t, "POST", "/v1/listings", body, bearer(token), newKey()), http.StatusCreated)
}

// settled polls the listing as its seller until it leaves processing.
func settled(t *testing.T, token, id string) listing {
	t.Helper()
	var l listing
	testenv.Eventually(t, 30*time.Second, func() error {
		if l = expect[listing](t, call(t, "GET", "/v1/listings/"+id, nil, bearer(token)), http.StatusOK); l.Status == "processing" {
			return fmt.Errorf("listing %s still processing", id)
		}
		return nil
	})
	return l
}

// activeListing posts a listing as postListing does and waits until it is
// on sale.
func activeListing(t *testing.T, token string, fields map[string]any) listing {
	t.Helper()
	// A seller with a listing on sale has linked a carrier to ship it.
	linkCarriers(t, token)
	l := settled(t, token, postListing(t, token, fields).ID)
	if l.Status != "active" {
		t.Fatalf("%+v", l)
	}
	return l
}

// activeHidden posts a listing as token, hides it and returns its ID.
func activeHidden(t *testing.T, token string) string {
	t.Helper()
	l := activeListing(t, token, nil)
	expectStatus(t, call(t, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(token)), http.StatusOK)
	return l.ID
}

func TestListingGoesOnSale(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	posted := postListing(t, seller.AccessToken, nil)
	if posted.Status != "processing" || posted.Location.Province.DisplayName != "Thành phố Hà Nội" {
		t.Fatalf("%+v", posted)
	}
	expectStatus(t, call(t, "GET", "/v1/listings/"+posted.ID, nil), http.StatusNotFound) // a buyer does not see a listing in processing
	l := settled(t, seller.AccessToken, posted.ID)
	if l.Status != "active" || l.Photos[0].URL == "" || l.Photos[0].ThumbURL == "" {
		t.Fatalf("%+v", l)
	}
	buyer, _ := newAccount(t)
	for _, h := range [][][2]string{nil, {bearer(buyer.AccessToken)}} {
		res := call(t, "GET", "/v1/listings/"+posted.ID, nil, h...)
		expectStatus(t, res, http.StatusOK)
		if bytes.Contains(res.Body, []byte(`"marks"`)) {
			t.Fatalf("a buyer sees marks: %s", res.Body)
		}
	}
	for _, u := range []string{l.Photos[0].URL, l.Photos[0].ThumbURL} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, u, http.NoBody)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/jpeg" || hasExif(b) {
			t.Fatalf("%s: %d %s exif %v", u, res.StatusCode, res.Header.Get("Content-Type"), hasExif(b))
		}
	}
	expectStatus(t, call(t, "GET", "/v1/listings/"+posted.ID, nil, bearer("junk")), http.StatusUnauthorized)
	expectStatus(t, call(t, "GET", "/v1/listings/not-a-uuid", nil), http.StatusBadRequest)
}

func TestPostListingRefusals(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := seller.AccessToken
	post := func(body map[string]any, headers ...[2]string) Response {
		t.Helper()
		return call(t, "POST", "/v1/listings", body, append([][2]string{newKey()}, headers...)...)
	}
	photo := photoWithGPS(t)
	check(t, "signed out", post(draftWith(upload(t, token, photo))), 401, "unauthenticated")
	bad := draftWith("not-a-uuid")
	check(t, "a bad photo id", post(bad, bearer(token)), 400, "invalid_request")
	marked := draftWith(upload(t, token, photo))
	marked["marks"] = []any{map[string]any{"text": "Trầy góc"}}
	check(t, "declared marks", post(marked, bearer(token)), 400, "invalid_request")

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "suspended", post(draftWith(upload(t, suspended.AccessToken, photo)), bearer(suspended.AccessToken)),
		403, "account_suspended")

	cheap := draftWith(upload(t, token, photo))
	cheap["price"] = 9_999
	res := post(cheap, bearer(token))
	check(t, "too cheap", res, 422, "invalid_listing")
	if res.Status == 422 && errorOf(t, res).Fields["price"] == "" {
		t.Errorf("no price field: %s", res.Body)
	}
	other, _ := newAccount(t)
	check(t, "another account's upload", post(draftWith(upload(t, other.AccessToken, photo)), bearer(token)),
		422, "invalid_upload")
	ticket := call(t, "POST", "/v1/uploads", map[string]any{"content_type": "image/jpeg", "size": 100}, bearer(token))
	check(t, "never uploaded", post(draftWith(decode[uploadTicket](t, ticket).UploadID), bearer(token)), 422, "invalid_upload")
	used := postListing(t, token, nil).Photos[0].UploadID
	check(t, "already attached", post(draftWith(used), bearer(token)), 422, "invalid_upload")

	full, _ := newAccount(t)
	first := postListing(t, full.AccessToken, nil)
	if _, err := db.Exec(context.Background(), `
INSERT INTO listings (id, seller_id, title, price, description, category_code, condition, allow_offers, commune_code,
                      photos, specs, status, rejection, write_id, created_at, updated_at, lat, lng)
SELECT gen_random_uuid(), seller_id, title, price, description, category_code, condition, allow_offers, commune_code,
       photos, specs, 'hidden', rejection, gen_random_uuid(), created_at, updated_at, lat, lng
FROM listings, generate_series(1, 19) n WHERE id = @id`, pgx.StrictNamedArgs{"id": first.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "at the cap", post(draftWith(upload(t, full.AccessToken, photo)), bearer(full.AccessToken)),
		409, "listing_cap_reached")
}

// A repeated Idempotency-Key returns the listing the first post made.
func TestPostListingTwice(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	key := newKey()
	body := draftWith(upload(t, seller.AccessToken, photoWithGPS(t)))
	first := call(t, "POST", "/v1/listings", body, bearer(seller.AccessToken), key)
	second := call(t, "POST", "/v1/listings", body, bearer(seller.AccessToken), key)
	if a, b := expect[listing](t, first, 201), expect[listing](t, second, 201); a.ID != b.ID {
		t.Fatalf("%s / %s", first.Body, second.Body)
	}
}

func TestListingTransitions(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := seller.AccessToken
	id := activeListing(t, token, nil).ID
	other, _ := newAccount(t)
	act := func(action, token string) Response {
		t.Helper()
		return call(t, "POST", "/v1/listings/"+id+"/"+action, nil, bearer(token))
	}
	for _, s := range []struct{ action, conflictFirst, status string }{
		{"hide", "unhide", "hidden"},
		{"unhide", "", "active"},
		{"mark-sold", "unmark-sold", "sold"},
		{"unmark-sold", "", "active"},
	} {
		check(t, s.action+" signed out", act(s.action, ""), 401, "")
		check(t, s.action+" by another account", act(s.action, other.AccessToken), 404, "listing_not_found")
		check(t, s.action+" bad id", call(t, "POST", "/v1/listings/not-a-uuid/"+s.action, nil, bearer(token)), 400, "")
		if s.conflictFirst != "" {
			check(t, s.conflictFirst+" before "+s.action, act(s.conflictFirst, token), 409, "listing_state_conflict")
		}
		if got := expect[listing](t, act(s.action, token), 200); got.Status != s.status {
			t.Fatalf("%s: %+v", s.action, got)
		}
		if s.action == "hide" {
			check(t, "another account sees a hidden listing", call(t, "GET", "/v1/listings/"+id, nil, bearer(other.AccessToken)), 404, "")
		}
	}
	check(t, "unmark an active listing", act("unmark-sold", token), 409, "")
	check(t, "unhide an active listing", act("unhide", token), 409, "")
	expectStatus(t, act("mark-sold", token), 200)
	for _, action := range []string{"hide", "mark-sold"} {
		check(t, action+" a sold listing", act(action, token), 409, "listing_state_conflict")
	}

	del := func(path, token string) Response {
		t.Helper()
		return call(t, "DELETE", path, nil, bearer(token))
	}
	check(t, "delete signed out", del("/v1/listings/"+id, ""), 401, "")
	check(t, "delete bad id", del("/v1/listings/not-a-uuid", token), 400, "")
	check(t, "delete by another account", del("/v1/listings/"+id, other.AccessToken), 404, "")
	expectStatus(t, del("/v1/listings/"+id, token), 204)
	check(t, "delete twice", del("/v1/listings/"+id, token), 409, "listing_state_conflict")
	check(t, "the seller sees a deleted listing", call(t, "GET", "/v1/listings/"+id, nil, bearer(token)), 404, "")
}

func TestUpdateListing(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := seller.AccessToken
	l := activeListing(t, token, nil)
	path := "/v1/listings/" + l.ID
	body := draftWith(l.Photos[0].UploadID)
	body["title"] = "Xe đạp Asama 26 inch"
	if got := expect[listing](t, call(t, "PUT", path, body, bearer(token), newKey()), 200); got.Status != "active" {
		t.Fatalf("a text edit: %+v", got)
	}
	body["photos"] = []string{l.Photos[0].UploadID, upload(t, token, photoWithGPS(t))}
	if got := expect[listing](t, call(t, "PUT", path, body, bearer(token), newKey()), 200); got.Status != "processing" {
		t.Fatalf("a photo added: %+v", got)
	}
	if got := settled(t, token, l.ID); got.Status != "active" || len(got.Photos) != 2 || got.Photos[1].URL == "" {
		t.Fatalf("%+v", got)
	}

	check(t, "signed out", call(t, "PUT", path, body, newKey()), 401, "")
	check(t, "bad id", call(t, "PUT", "/v1/listings/not-a-uuid", body, bearer(token), newKey()), 400, "")
	other, _ := newAccount(t)
	check(t, "another account", call(t, "PUT", path, body, bearer(other.AccessToken), newKey()), 404, "")
	cheap := draftWith(l.Photos[0].UploadID)
	cheap["price"] = 1
	check(t, "too cheap", call(t, "PUT", path, cheap, bearer(token), newKey()), 422, "invalid_listing")
	expectStatus(t, call(t, "POST", path+"/mark-sold", nil, bearer(token)), 200)
	check(t, "edit a sold listing", call(t, "PUT", path, body, bearer(token), newKey()), 409, "listing_state_conflict")
	suspend(t, seller.Account.ID)
	check(t, "suspended", call(t, "PUT", path, body, bearer(token), newKey()), 403, "account_suspended")
}

func TestCategoriesAndRegions(t *testing.T) {
	t.Parallel()
	if n := len(expect[page[any]](t, call(t, "GET", "/v1/categories", nil), 200).Items); n != 55 {
		t.Fatalf("categories: %d items", n)
	}
	if n := len(expect[page[any]](t, call(t, "GET", "/v1/regions", nil), 200).Items); n != 34 {
		t.Fatalf("provinces: %d items", n)
	}
	if n := len(expect[page[any]](t, call(t, "GET", "/v1/regions/01/communes", nil), 200).Items); n == 0 {
		t.Fatalf("communes of Hà Nội: %d items", n)
	}
	expectStatus(t, call(t, "GET", "/v1/regions/hanoi/communes", nil), 400)
}

// The commune nearest a point comes with its province; a point far from
// every commune, or a bad one, is refused.
func TestNearestCommune(t *testing.T) {
	t.Parallel()
	type commune struct {
		Code     string
		Lat, Lng float64
	}
	at := expect[page[commune]](t, call(t, "GET", "/v1/regions/01/communes", nil), 200).Items[0]
	res := call(t, "GET", fmt.Sprintf("/v1/regions/nearest?lat=%v&lng=%v", at.Lat, at.Lng), nil)
	type region struct {
		Code string `json:"code"`
		Name string `json:"display_name"`
	}
	got := expect[struct{ Commune, Province region }](t, res, 200)
	if got.Commune.Code != at.Code || got.Province.Code != "01" || got.Province.Name == "" {
		t.Fatalf("%s", res.Body)
	}
	check(t, "Paris", call(t, "GET", "/v1/regions/nearest?lat=48.85&lng=2.35", nil), 404, "no_commune_near")
	check(t, "no lng", call(t, "GET", "/v1/regions/nearest?lat=21.03", nil), 400, "invalid_request")
	check(t, "lat out of range", call(t, "GET", "/v1/regions/nearest?lat=95&lng=105.8", nil), 400, "invalid_request")
}

// A GPS point far from the commune is refused, a near one is taken, and no
// response shows the point.
func TestListingGPS(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	body := draftWith(upload(t, seller.AccessToken, photoWithGPS(t)))
	body["gps"] = map[string]any{"lat": 10.77, "lng": 106.70} // TP.HCM; commune 00004 is in Hà Nội
	res := call(t, "POST", "/v1/listings", body, bearer(seller.AccessToken), newKey())
	if e := errorOf(t, res); res.Status != 422 || e.Code != "invalid_listing" || e.Fields["gps"] == "" {
		t.Fatalf("far: %d %s", res.Status, res.Body)
	}
	body = draftWith(upload(t, seller.AccessToken, photoWithGPS(t)))
	body["gps"] = map[string]any{"lat": 21.0333, "lng": 105.8222}
	res = call(t, "POST", "/v1/listings", body, bearer(seller.AccessToken), newKey())
	got := expect[map[string]any](t, res, http.StatusCreated)
	location, ok := got["location"].(map[string]any)
	if _, leaked := got["lat"]; leaked || !ok || got["lng"] != nil || got["gps"] != nil || location["lat"] == 21.03 {
		t.Fatalf("the point leaked: %s", res.Body)
	}
}
