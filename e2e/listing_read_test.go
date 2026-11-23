package e2e

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/testenv"
)

type listingRead struct {
	ID            string `json:"id"`
	Source        string `json:"source"`
	Status        string `json:"status"`
	Failure       string `json:"failure"`
	FailureReason string `json:"failure_reason"`
	Link          *struct {
		Site       string `json:"site"`
		URL        string `json:"url"`
		PhotoCount int    `json:"photo_count"`
	} `json:"link"`
	Photos []struct {
		UploadID  string   `json:"upload_id"`
		URL       string   `json:"url"`
		AtSeconds *float64 `json:"at_seconds"`
	} `json:"photos"`
	Draft *struct {
		Title        string              `json:"title"`
		CategoryCode string              `json:"category_code"`
		Condition    string              `json:"condition"`
		Specs        []map[string]string `json:"specs"`
		Description  string              `json:"description"`
		Price        *int64              `json:"price"`
		AllowOffers  bool                `json:"allow_offers"`
		Photos       []string            `json:"photos"`
	} `json:"draft"`
	ContactRemoved *bool `json:"contact_removed"`
}

// The specs a seller fills for a bicycle and an iPhone, the categories'
// specs by their Vietnamese names.
var (
	bicycleSpecs = []map[string]string{{"label": "Hãng", "value": "Asama"}, {"label": "Loại xe", "value": "Xe đạp phố"}, {"label": "Cỡ khung", "value": "26 inch"}}
	iphoneSpecs  = []map[string]string{{"label": "Dòng máy", "value": "iPhone 11"}, {"label": "Dung lượng", "value": "64 GB"}, {"label": "Màu", "value": "Trắng"}, {"label": "Pin", "value": "81%"}}
)

// startRead starts a read of body as the account with token.
func startRead(t *testing.T, token string, body map[string]any) listingRead {
	t.Helper()
	return expect[listingRead](t, call(t, "POST", "/v1/listing-reads", body, bearer(token), newKey()), http.StatusAccepted)
}

// startVideoRead starts a read of video.
func startVideoRead(t *testing.T, token, video string) listingRead {
	t.Helper()
	return startRead(t, token, map[string]any{"video": video})
}

// readEnd polls the read until it leaves reading.
func readEnd(t *testing.T, token, id string) listingRead {
	t.Helper()
	var r listingRead
	testenv.Eventually(t, 30*time.Second, func() error {
		if r = expect[listingRead](t, call(t, "GET", "/v1/listing-reads/"+id, nil, bearer(token)), http.StatusOK); r.Status == "reading" {
			return fmt.Errorf("read %s still reading", id)
		}
		return nil
	})
	return r
}

// uploadStatus is an upload's status in the database.
func uploadStatus(t *testing.T, id string) string {
	t.Helper()
	var s string
	if err := db.QueryRow(context.Background(), `SELECT status FROM uploads WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// A seller's video reads to a draft whose kept frames post as the photos of
// a listing that goes on sale.
func TestVideoReadToListing(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	video := uploadVideo(t, seller.AccessToken, "a phone video of a bicycle")
	started := startVideoRead(t, seller.AccessToken, video)
	if started.Status != "reading" || started.Draft != nil {
		t.Fatalf("started: %+v", started)
	}
	r := readEnd(t, seller.AccessToken, started.ID)
	if r.Status != "done" || r.Source != "video" || r.Link != nil || len(r.Photos) != 3 || r.Draft == nil {
		t.Fatalf("%+v", r)
	}
	d := r.Draft
	if d.Title != "Xe đạp Asama xanh" || d.CategoryCode != "vehicles.bicycle" || d.Condition != "used" ||
		d.Price != nil || d.AllowOffers || len(d.Specs) != 0 || d.Description != "Xe đi ít, có thương lượng." ||
		!slices.Equal(d.Photos, []string{r.Photos[2].UploadID, r.Photos[0].UploadID, r.Photos[1].UploadID}) {
		t.Fatalf("draft %+v", d)
	}
	if s := uploadStatus(t, video); s != "consumed" {
		t.Fatalf("the video is %s", s)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, r.Photos[0].URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("frame URL: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}

	log := expect[struct {
		Updates []struct {
			Kind          string `json:"kind"`
			ListingReadID string `json:"listing_read_id"`
		} `json:"updates"`
	}](t, call(t, "GET", "/v1/updates?since=0", nil, bearer(seller.AccessToken)), http.StatusOK)
	n := 0
	for _, u := range log.Updates {
		if u.Kind == "listing_read" && u.ListingReadID == r.ID {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("%d listing_read updates, want start, frames and done", n)
	}

	body := map[string]any{
		"title": d.Title, "price": 1_200_000, "description": d.Description, "category_code": d.CategoryCode,
		"condition": d.Condition, "allow_offers": d.AllowOffers, "commune_code": "00004", "photos": d.Photos, "specs": bicycleSpecs,
	}
	l := expect[listing](t, call(t, "POST", "/v1/listings", body, bearer(seller.AccessToken), newKey()), http.StatusCreated)
	if l = settled(t, seller.AccessToken, l.ID); l.Status != "active" || len(l.Photos) != 3 || l.Photos[0].UploadID != d.Photos[0] {
		t.Fatalf("listing %+v", l)
	}
}

// A read that ends early says why and consumes the video.
func TestVideoReadFailures(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, video, failure, reason string
		frames                       int
	}{
		{"too short", shortVideo, "video_too_short", "", 0},
		{"no item", emptyRoom, "no_item", "Video chỉ có một căn phòng trống.", 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			seller, _ := newAccount(t)
			video := uploadVideo(t, seller.AccessToken, c.video)
			r := readEnd(t, seller.AccessToken, startVideoRead(t, seller.AccessToken, video).ID)
			if r.Status != "failed" || r.Failure != c.failure || r.FailureReason != c.reason || len(r.Photos) != c.frames || r.Draft != nil {
				t.Fatalf("%+v", r)
			}
			if s := uploadStatus(t, video); s != "consumed" {
				t.Fatalf("the video is %s", s)
			}
		})
	}
}

func TestCreateListingReadRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	post := func(token, video string, key [2]string) Response {
		return call(t, "POST", "/v1/listing-reads", map[string]any{"video": video}, bearer(token), key)
	}

	check(t, "signed out", post("", uuid.NewV7().String(), newKey()), 401, "unauthenticated")
	check(t, "no video", call(t, "POST", "/v1/listing-reads", map[string]any{}, bearer(me.AccessToken), newKey()), 400, "")
	check(t, "a long transcript", call(t, "POST", "/v1/listing-reads",
		map[string]any{"video": uploadVideo(t, me.AccessToken, "a video"), "transcript": strings.Repeat("a", 1001)},
		bearer(me.AccessToken), newKey()), 400, "invalid_request")
	check(t, "two sources", call(t, "POST", "/v1/listing-reads",
		map[string]any{"video": uuid.NewV7().String(), "link": iphoneURL(iphoneListing)}, bearer(me.AccessToken), newKey()), 400, "invalid_request")
	check(t, "a photo", post(me.AccessToken, upload(t, me.AccessToken, photoWithGPS(t)), newKey()), 422, "invalid_upload")
	check(t, "someone else's video", post(me.AccessToken, uploadVideo(t, other.AccessToken, "their video"), newKey()),
		422, "invalid_upload")

	video := uploadVideo(t, me.AccessToken, "a phone video of a bicycle")
	key := newKey()
	first := expect[listingRead](t, post(me.AccessToken, video, key), http.StatusAccepted)
	if again := expect[listingRead](t, post(me.AccessToken, video, key), http.StatusAccepted); again.ID != first.ID {
		t.Fatalf("a repeated key started %s after %s", again.ID, first.ID)
	}
	check(t, "read twice", post(me.AccessToken, video, newKey()), 422, "invalid_upload")

	suspended, _ := newAccount(t)
	theirs := uploadVideo(t, suspended.AccessToken, "a phone video of a bicycle")
	suspend(t, suspended.Account.ID)
	check(t, "suspended", post(suspended.AccessToken, theirs, newKey()), 403, "account_suspended")

	busy, _ := newAccount(t)
	if _, err := db.Exec(context.Background(), `INSERT INTO listing_reads
    (id, account_id, source, media_id, status, created_at, expires_at, write_id)
SELECT gen_random_uuid(), @account, 'video', gen_random_uuid(), 'reading', now(), now() + interval '1 day', gen_random_uuid()
FROM generate_series(1, 30)`, pgx.StrictNamedArgs{"account": busy.Account.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "thirty reads", post(busy.AccessToken, uploadVideo(t, busy.AccessToken, "one more video"), newKey()),
		429, "read_limit_reached")
}

func TestGetListingRead(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	r := readEnd(t, me.AccessToken, startVideoRead(t, me.AccessToken, uploadVideo(t, me.AccessToken, "a phone video of a bicycle")).ID)
	path := "/v1/listing-reads/" + r.ID

	check(t, "another account", call(t, "GET", path, nil, bearer(other.AccessToken)), 404, "listing_read_not_found")
	check(t, "signed out", call(t, "GET", path, nil), 401, "unauthenticated")
	check(t, "not a uuid", call(t, "GET", "/v1/listing-reads/nope", nil, bearer(me.AccessToken)), 400, "")
	if _, err := db.Exec(context.Background(), `UPDATE listing_reads SET expires_at = now() - interval '1 minute' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": r.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "expired", call(t, "GET", path, nil, bearer(me.AccessToken)), 404, "listing_read_not_found")
}

// A deleted account's reads go with it.
func TestDeletedAccountsReadsGo(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	readEnd(t, me.AccessToken, startVideoRead(t, me.AccessToken, uploadVideo(t, me.AccessToken, "a phone video of a bicycle")).ID)
	check(t, "delete", call(t, "DELETE", "/v1/accounts/me", nil, bearer(me.AccessToken)), 204, "")
	testenv.Eventually(t, 30*time.Second, func() error {
		var n int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM listing_reads WHERE account_id = @a`,
			pgx.StrictNamedArgs{"a": me.Account.ID}).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			return fmt.Errorf("%d reads left", n)
		}
		return nil
	})
}

// With the phone's transcript the read takes the seller's words from it,
// not from the video's sound: the empty room said by the phone gives
// no_item although the video is the bicycle. Nothing of it is left.
func TestVideoReadWithThePhonesTranscript(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	video := uploadVideo(t, seller.AccessToken, "a phone video of a bicycle")
	r := readEnd(t, seller.AccessToken, startRead(t, seller.AccessToken,
		map[string]any{"video": video, "transcript": "Trong phòng không có gì."}).ID)
	if r.Status != "failed" || r.Failure != "no_item" {
		t.Fatalf("%+v", r)
	}
	var left int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE transcript IS NOT NULL) +
       (SELECT count(*) FROM listing_read_transcripts WHERE account_id = @a) FROM listing_reads WHERE account_id = @a`,
		pgx.StrictNamedArgs{"a": seller.Account.ID}).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d transcripts left: %v", left, err)
	}
}

// A photo read fills a draft whose one photo is the photo, and the draft
// posts as a listing with a price added.
func TestPhotoReadToListing(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	photo := upload(t, seller.AccessToken, frameJPEG(1))
	r := readEnd(t, seller.AccessToken, startRead(t, seller.AccessToken, map[string]any{"photo": photo}).ID)
	if r.Status != "done" || r.Source != "photo" || len(r.Photos) != 1 || r.Photos[0].UploadID != photo ||
		r.Photos[0].AtSeconds != nil || r.Draft == nil {
		t.Fatalf("%+v", r)
	}
	d := r.Draft
	if d.Title != "Xe đạp Asama xanh" || d.Price != nil || d.AllowOffers || !slices.Equal(d.Photos, []string{photo}) {
		t.Fatalf("draft %+v", d)
	}
	if s := uploadStatus(t, photo); s != "waiting" {
		t.Fatalf("the photo is %s", s)
	}
	body := map[string]any{
		"title": d.Title, "price": 1_200_000, "description": d.Description, "category_code": d.CategoryCode,
		"condition": d.Condition, "allow_offers": d.AllowOffers, "commune_code": "00004", "photos": d.Photos, "specs": bicycleSpecs,
	}
	l := expect[listing](t, call(t, "POST", "/v1/listings", body, bearer(seller.AccessToken), newKey()), http.StatusCreated)
	if l = settled(t, seller.AccessToken, l.ID); l.Status != "active" || len(l.Photos) != 1 {
		t.Fatalf("listing %+v", l)
	}
}

func TestCreatePhotoReadRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	post := func(token, photo string) Response {
		return call(t, "POST", "/v1/listing-reads", map[string]any{"photo": photo}, bearer(token), newKey())
	}
	check(t, "a video", post(me.AccessToken, uploadVideo(t, me.AccessToken, "a video")), 422, "invalid_upload")
	check(t, "someone else's photo", post(me.AccessToken, upload(t, other.AccessToken, frameJPEG(0))), 422, "invalid_upload")

	busy, _ := newAccount(t)
	if _, err := db.Exec(context.Background(), `INSERT INTO listing_reads
    (id, account_id, source, media_id, status, created_at, expires_at, write_id)
SELECT gen_random_uuid(), @account, 'photo', gen_random_uuid(), 'reading', now(), now() + interval '1 day', gen_random_uuid()
FROM generate_series(1, 60)`, pgx.StrictNamedArgs{"account": busy.Account.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "sixty photo reads", post(busy.AccessToken, upload(t, busy.AccessToken, frameJPEG(2))), 429, "read_limit_reached")
	startVideoRead(t, busy.AccessToken, uploadVideo(t, busy.AccessToken, "a phone video of a bicycle"))
}

// A read's event stream sends the read, the text as the model writes it,
// and the read again when it ends; a read that ended gets one event.
func TestListingReadEvents(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	other, _ := newAccount(t)
	started := startRead(t, seller.AccessToken, map[string]any{"photo": upload(t, seller.AccessToken, frameJPEG(1))})
	path := "/v1/listing-reads/" + started.ID + "/events"
	res := call(t, "GET", path, nil, bearer(seller.AccessToken))
	expectStatus(t, res, http.StatusOK)
	body := string(res.Body)
	if !strings.HasPrefix(body, "event: read\ndata: {") || !strings.Contains(body, `"status":"done"`) ||
		!strings.HasSuffix(body, "\n\n") {
		t.Fatalf("events %s", body)
	}
	if !strings.Contains(body, `"title":"Xe đạp Asama xanh"`) {
		t.Fatalf("no title in %s", body)
	}
	again := string(call(t, "GET", path, nil, bearer(seller.AccessToken)).Body)
	if strings.Count(again, "event: ") != 1 || !strings.Contains(again, `"status":"done"`) {
		t.Fatalf("an ended read: %s", again)
	}
	expectStatus(t, call(t, "GET", path, nil, bearer(other.AccessToken)), http.StatusNotFound)
	expectStatus(t, call(t, "GET", path, nil), http.StatusUnauthorized)
	expectStatus(t, call(t, "GET", "/v1/listing-reads/nope/events", nil, bearer(seller.AccessToken)), http.StatusBadRequest)
}
