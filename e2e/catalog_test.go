package e2e

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type listing struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Photos []struct {
		UploadID string `json:"upload_id"`
		URL      string `json:"url"`
		ThumbURL string `json:"thumb_url"`
	} `json:"photos"`
	Location struct {
		Province struct {
			DisplayName string `json:"display_name"`
		} `json:"province"`
	} `json:"location"`
}

type listingList struct {
	Items []listing `json:"items"`
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

// postListing posts a listing with one fresh photo as the account with token.
func postListing(t *testing.T, token string) listing {
	t.Helper()
	res := call(t, server, "POST", "/v1/listings", draftWith(upload(t, token, photoWithGPS(t))),
		bearer(token), newKey())
	if res.Status != http.StatusCreated {
		t.Fatalf("post: %d %s", res.Status, res.Body)
	}
	return decode[listing](t, res)
}

// settled polls the listing as its seller until it leaves processing.
func settled(t *testing.T, token, id string) listing {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		res := call(t, server, "GET", "/v1/listings/"+id, nil, bearer(token))
		if res.Status != http.StatusOK {
			t.Fatalf("get: %d %s", res.Status, res.Body)
		}
		if l := decode[listing](t, res); l.Status != "processing" {
			return l
		}
		if time.Now().After(deadline) {
			t.Fatalf("listing %s still processing", id)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// activeListing posts a listing and waits until it is on sale.
func activeListing(t *testing.T, token string) listing {
	t.Helper()
	l := settled(t, token, postListing(t, token).ID)
	if l.Status != "active" {
		t.Fatalf("%+v", l)
	}
	return l
}

func TestListingGoesOnSale(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	posted := postListing(t, seller.AccessToken)
	if posted.Status != "processing" || posted.Location.Province.DisplayName != "Thành phố Hà Nội" {
		t.Fatalf("%+v", posted)
	}
	if res := call(t, server, "GET", "/v1/listings/"+posted.ID, nil); res.Status != http.StatusNotFound {
		t.Fatalf("a buyer sees a listing in processing: %d", res.Status)
	}
	l := settled(t, seller.AccessToken, posted.ID)
	if l.Status != "active" || l.Photos[0].URL == "" || l.Photos[0].ThumbURL == "" {
		t.Fatalf("%+v", l)
	}
	buyer, _ := newAccount(t)
	for _, h := range [][][2]string{nil, {bearer(buyer.AccessToken)}} {
		if res := call(t, server, "GET", "/v1/listings/"+posted.ID, nil, h...); res.Status != http.StatusOK ||
			bytes.Contains(res.Body, []byte(`"marks"`)) {
			t.Fatalf("a buyer: %d %s", res.Status, res.Body)
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
	if res := call(t, server, "GET", "/v1/listings/"+posted.ID, nil, bearer("junk")); res.Status != http.StatusUnauthorized {
		t.Fatalf("a bad token: %d", res.Status)
	}
	if res := call(t, server, "GET", "/v1/listings/not-a-uuid", nil); res.Status != http.StatusBadRequest {
		t.Fatalf("a bad id: %d", res.Status)
	}
}

func TestPostListingRefusals(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := seller.AccessToken
	post := func(body map[string]any, headers ...[2]string) Response {
		t.Helper()
		return call(t, server, "POST", "/v1/listings", body, append([][2]string{newKey()}, headers...)...)
	}
	expect := func(name string, res Response, status int, code string) {
		t.Helper()
		if res.Status != status || errorOf(t, res).Code != code {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	photo := photoWithGPS(t)
	expect("signed out", post(draftWith(upload(t, token, photo))), 401, "unauthenticated")
	bad := draftWith("not-a-uuid")
	expect("a bad photo id", post(bad, bearer(token)), 400, "invalid_request")
	marked := draftWith(upload(t, token, photo))
	marked["marks"] = []any{map[string]any{"text": "Trầy góc"}}
	expect("declared marks", post(marked, bearer(token)), 400, "invalid_request")

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	expect("suspended", post(draftWith(upload(t, suspended.AccessToken, photo)), bearer(suspended.AccessToken)),
		403, "account_suspended")

	cheap := draftWith(upload(t, token, photo))
	cheap["price"] = 9_999
	res := post(cheap, bearer(token))
	expect("too cheap", res, 422, "invalid_listing")
	if res.Status == 422 && errorOf(t, res).Fields["price"] == "" {
		t.Errorf("no price field: %s", res.Body)
	}
	other, _ := newAccount(t)
	expect("another account's upload", post(draftWith(upload(t, other.AccessToken, photo)), bearer(token)),
		422, "invalid_upload")
	ticket := call(t, server, "POST", "/v1/uploads", map[string]any{"content_type": "image/jpeg", "size": 100}, bearer(token))
	expect("never uploaded", post(draftWith(decode[uploadTicket](t, ticket).UploadID), bearer(token)), 422, "invalid_upload")
	used := postListing(t, token).Photos[0].UploadID
	expect("already attached", post(draftWith(used), bearer(token)), 422, "invalid_upload")

	full, _ := newAccount(t)
	first := postListing(t, full.AccessToken)
	if _, err := db.Exec(context.Background(), `
INSERT INTO listings (id, seller_id, title, price, description, category_code, condition, allow_offers, commune_code,
                      photos, specs, status, rejection, write_id, created_at, updated_at, lat, lng)
SELECT gen_random_uuid(), seller_id, title, price, description, category_code, condition, allow_offers, commune_code,
       photos, specs, 'hidden', rejection, gen_random_uuid(), created_at, updated_at, lat, lng
FROM listings, generate_series(1, 19) n WHERE id = @id`, pgx.StrictNamedArgs{"id": first.ID}); err != nil {
		t.Fatal(err)
	}
	expect("at the cap", post(draftWith(upload(t, full.AccessToken, photo)), bearer(full.AccessToken)),
		409, "listing_cap_reached")
}

// A repeated Idempotency-Key returns the listing the first post made.
func TestPostListingTwice(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	key := newKey()
	body := draftWith(upload(t, seller.AccessToken, photoWithGPS(t)))
	first := call(t, server, "POST", "/v1/listings", body, bearer(seller.AccessToken), key)
	second := call(t, server, "POST", "/v1/listings", body, bearer(seller.AccessToken), key)
	if first.Status != 201 || second.Status != 201 || decode[listing](t, first).ID != decode[listing](t, second).ID {
		t.Fatalf("%d %s / %d %s", first.Status, first.Body, second.Status, second.Body)
	}
}

func TestListingTransitions(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := seller.AccessToken
	id := activeListing(t, token).ID
	other, _ := newAccount(t)
	act := func(action, token string) Response {
		t.Helper()
		var h [][2]string
		if token != "" {
			h = append(h, bearer(token))
		}
		return call(t, server, "POST", "/v1/listings/"+id+"/"+action, nil, h...)
	}
	for _, s := range []struct{ action, conflictFirst, status string }{
		{"hide", "unhide", "hidden"},
		{"unhide", "", "active"},
		{"mark-sold", "unmark-sold", "sold"},
		{"unmark-sold", "", "active"},
	} {
		if res := act(s.action, ""); res.Status != 401 {
			t.Errorf("%s signed out: %d", s.action, res.Status)
		}
		if res := act(s.action, other.AccessToken); res.Status != 404 || errorOf(t, res).Code != "listing_not_found" {
			t.Errorf("%s by another account: %d %s", s.action, res.Status, res.Body)
		}
		if res := call(t, server, "POST", "/v1/listings/not-a-uuid/"+s.action, nil, bearer(token)); res.Status != 400 {
			t.Errorf("%s bad id: %d", s.action, res.Status)
		}
		if s.conflictFirst != "" {
			if res := act(s.conflictFirst, token); res.Status != 409 || errorOf(t, res).Code != "listing_state_conflict" {
				t.Errorf("%s before %s: %d %s", s.conflictFirst, s.action, res.Status, res.Body)
			}
		}
		res := act(s.action, token)
		if res.Status != 200 || decode[listing](t, res).Status != s.status {
			t.Fatalf("%s: %d %s", s.action, res.Status, res.Body)
		}
		if s.action == "hide" {
			if res = call(t, server, "GET", "/v1/listings/"+id, nil, bearer(other.AccessToken)); res.Status != 404 {
				t.Errorf("another account sees a hidden listing: %d", res.Status)
			}
		}
	}
	if res := act("unmark-sold", token); res.Status != 409 {
		t.Errorf("unmark an active listing: %d", res.Status)
	}
	if res := act("unhide", token); res.Status != 409 {
		t.Errorf("unhide an active listing: %d", res.Status)
	}
	if res := act("mark-sold", token); res.Status != 200 {
		t.Fatalf("mark sold: %d", res.Status)
	}
	for _, action := range []string{"hide", "mark-sold"} {
		if res := act(action, token); res.Status != 409 || errorOf(t, res).Code != "listing_state_conflict" {
			t.Errorf("%s a sold listing: %d %s", action, res.Status, res.Body)
		}
	}

	del := func(path, token string) Response {
		t.Helper()
		var h [][2]string
		if token != "" {
			h = append(h, bearer(token))
		}
		return call(t, server, "DELETE", path, nil, h...)
	}
	if res := del("/v1/listings/"+id, ""); res.Status != 401 {
		t.Errorf("delete signed out: %d", res.Status)
	}
	if res := del("/v1/listings/not-a-uuid", token); res.Status != 400 {
		t.Errorf("delete bad id: %d", res.Status)
	}
	if res := del("/v1/listings/"+id, other.AccessToken); res.Status != 404 {
		t.Errorf("delete by another account: %d", res.Status)
	}
	if res := del("/v1/listings/"+id, token); res.Status != 204 {
		t.Fatalf("delete: %d %s", res.Status, res.Body)
	}
	if res := del("/v1/listings/"+id, token); res.Status != 409 || errorOf(t, res).Code != "listing_state_conflict" {
		t.Errorf("delete twice: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/listings/"+id, nil, bearer(token)); res.Status != 404 {
		t.Errorf("the seller sees a deleted listing: %d", res.Status)
	}
}

func TestUpdateListing(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := seller.AccessToken
	l := activeListing(t, token)
	path := "/v1/listings/" + l.ID
	body := draftWith(l.Photos[0].UploadID)
	body["title"] = "Xe đạp Asama 26 inch"
	res := call(t, server, "PUT", path, body, bearer(token), newKey())
	if res.Status != 200 || decode[listing](t, res).Status != "active" {
		t.Fatalf("a text edit: %d %s", res.Status, res.Body)
	}
	body["photos"] = []string{l.Photos[0].UploadID, upload(t, token, photoWithGPS(t))}
	if res = call(t, server, "PUT", path, body, bearer(token), newKey()); res.Status != 200 || decode[listing](t, res).Status != "processing" {
		t.Fatalf("a photo added: %d %s", res.Status, res.Body)
	}
	if got := settled(t, token, l.ID); got.Status != "active" || len(got.Photos) != 2 || got.Photos[1].URL == "" {
		t.Fatalf("%+v", got)
	}

	if res = call(t, server, "PUT", path, body, newKey()); res.Status != 401 {
		t.Errorf("signed out: %d", res.Status)
	}
	if res = call(t, server, "PUT", "/v1/listings/not-a-uuid", body, bearer(token), newKey()); res.Status != 400 {
		t.Errorf("bad id: %d", res.Status)
	}
	other, _ := newAccount(t)
	if res = call(t, server, "PUT", path, body, bearer(other.AccessToken), newKey()); res.Status != 404 {
		t.Errorf("another account: %d", res.Status)
	}
	cheap := draftWith(l.Photos[0].UploadID)
	cheap["price"] = 1
	if res = call(t, server, "PUT", path, cheap, bearer(token), newKey()); res.Status != 422 || errorOf(t, res).Code != "invalid_listing" {
		t.Errorf("too cheap: %d %s", res.Status, res.Body)
	}
	if res = call(t, server, "POST", path+"/mark-sold", nil, bearer(token)); res.Status != 200 {
		t.Fatalf("mark sold: %d", res.Status)
	}
	if res = call(t, server, "PUT", path, body, bearer(token), newKey()); res.Status != 409 || errorOf(t, res).Code != "listing_state_conflict" {
		t.Errorf("edit a sold listing: %d %s", res.Status, res.Body)
	}
	suspend(t, seller.Account.ID)
	if res = call(t, server, "PUT", path, body, bearer(token), newKey()); res.Status != 403 || errorOf(t, res).Code != "account_suspended" {
		t.Errorf("suspended: %d %s", res.Status, res.Body)
	}
}

func TestMyListings(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := seller.AccessToken
	onSale := activeListing(t, token).ID
	sold := activeListing(t, token).ID
	if res := call(t, server, "POST", "/v1/listings/"+sold+"/mark-sold", nil, bearer(token)); res.Status != 200 {
		t.Fatalf("mark sold: %d", res.Status)
	}
	for pane, want := range map[string]string{"on_sale": onSale, "sold": sold} {
		res := call(t, server, "GET", "/v1/me/listings?pane="+pane, nil, bearer(token))
		if items := decode[listingList](t, res).Items; res.Status != 200 || len(items) != 1 || items[0].ID != want {
			t.Errorf("%s: %d %s", pane, res.Status, res.Body)
		}
	}
	if res := call(t, server, "GET", "/v1/me/listings?pane=everything", nil, bearer(token)); res.Status != 400 {
		t.Errorf("unknown pane: %d", res.Status)
	}
	if res := call(t, server, "GET", "/v1/me/listings?pane=sold", nil); res.Status != 401 {
		t.Errorf("signed out: %d", res.Status)
	}
}

func TestCategoriesAndRegions(t *testing.T) {
	t.Parallel()
	res := call(t, server, "GET", "/v1/categories", nil)
	if n := len(decode[struct{ Items []any }](t, res).Items); res.Status != 200 || n != 55 {
		t.Fatalf("categories: %d, %d items", res.Status, n)
	}
	res = call(t, server, "GET", "/v1/regions", nil)
	if n := len(decode[struct{ Items []any }](t, res).Items); res.Status != 200 || n != 34 {
		t.Fatalf("provinces: %d, %d items", res.Status, n)
	}
	res = call(t, server, "GET", "/v1/regions/01/communes", nil)
	if n := len(decode[struct{ Items []any }](t, res).Items); res.Status != 200 || n == 0 {
		t.Fatalf("communes of Hà Nội: %d, %d items", res.Status, n)
	}
	if res = call(t, server, "GET", "/v1/regions/hanoi/communes", nil); res.Status != 400 {
		t.Fatalf("a bad code: %d", res.Status)
	}
}

// A GPS point far from the commune is refused, a near one is taken, and no
// response shows the point.
func TestListingGPS(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	body := draftWith(upload(t, seller.AccessToken, photoWithGPS(t)))
	body["gps"] = map[string]any{"lat": 10.77, "lng": 106.70} // TP.HCM; commune 00004 is in Hà Nội
	res := call(t, server, "POST", "/v1/listings", body, bearer(seller.AccessToken), newKey())
	if e := errorOf(t, res); res.Status != 422 || e.Code != "invalid_listing" || e.Fields["gps"] == "" {
		t.Fatalf("far: %d %s", res.Status, res.Body)
	}
	body = draftWith(upload(t, seller.AccessToken, photoWithGPS(t)))
	body["gps"] = map[string]any{"lat": 21.0333, "lng": 105.8222}
	res = call(t, server, "POST", "/v1/listings", body, bearer(seller.AccessToken), newKey())
	if res.Status != http.StatusCreated {
		t.Fatalf("near: %d %s", res.Status, res.Body)
	}
	got := decode[map[string]any](t, res)
	location, ok := got["location"].(map[string]any)
	if _, leaked := got["lat"]; leaked || !ok || got["lng"] != nil || got["gps"] != nil || location["lat"] == 21.03 {
		t.Fatalf("the point leaked: %s", res.Body)
	}
}
