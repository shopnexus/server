package e2e

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

type videoRead struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Failure string `json:"failure"`
	Frames  []struct {
		UploadID  string  `json:"upload_id"`
		URL       string  `json:"url"`
		AtSeconds float64 `json:"at_seconds"`
	} `json:"frames"`
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
	Estimate *struct {
		Low        int64  `json:"low"`
		Median     int64  `json:"median"`
		High       int64  `json:"high"`
		Source     string `json:"source"`
		SampleSize int    `json:"sample_size"`
	} `json:"estimate"`
}

// startRead starts a read of video as the account with token.
func startRead(t *testing.T, token, video string) videoRead {
	t.Helper()
	return expect[videoRead](t, call(t, "POST", "/v1/video-reads", map[string]any{"video": video}, bearer(token), newKey()),
		http.StatusAccepted)
}

// readEnd polls the read until it leaves reading.
func readEnd(t *testing.T, token, id string) videoRead {
	t.Helper()
	var r videoRead
	testenv.Eventually(t, 30*time.Second, func() error {
		if r = expect[videoRead](t, call(t, "GET", "/v1/video-reads/"+id, nil, bearer(token)), http.StatusOK); r.Status == "reading" {
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
	started := startRead(t, seller.AccessToken, video)
	if started.Status != "reading" || started.Draft != nil || started.Estimate != nil {
		t.Fatalf("started: %+v", started)
	}
	r := readEnd(t, seller.AccessToken, started.ID)
	if r.Status != "done" || len(r.Frames) != 3 || r.Draft == nil || r.Estimate == nil {
		t.Fatalf("%+v", r)
	}
	d := r.Draft
	if d.Title != "Xe đạp Asama xanh" || d.CategoryCode != "vehicles.bicycle" || d.Condition != "used" ||
		d.Price == nil || *d.Price != 1_200_000 || !d.AllowOffers ||
		!slices.Equal(d.Photos, []string{r.Frames[2].UploadID, r.Frames[0].UploadID}) {
		t.Fatalf("draft %+v", d)
	}
	// The embedding stub's vectors are below the floors, so no listing is
	// similar and the model's range stands.
	if r.Estimate.Source != "model" || r.Estimate.Median != 1_200_000 || r.Estimate.SampleSize != 0 {
		t.Fatalf("estimate %+v", r.Estimate)
	}
	if s := uploadStatus(t, video); s != "consumed" {
		t.Fatalf("the video is %s", s)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, r.Frames[0].URL, http.NoBody)
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
			Kind        string `json:"kind"`
			VideoReadID string `json:"video_read_id"`
		} `json:"updates"`
	}](t, call(t, "GET", "/v1/updates?since=0", nil, bearer(seller.AccessToken)), http.StatusOK)
	n := 0
	for _, u := range log.Updates {
		if u.Kind == "video_read" && u.VideoReadID == r.ID {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("%d video_read updates, want start, frames, draft and done", n)
	}

	body := map[string]any{
		"title": d.Title, "price": *d.Price, "description": d.Description, "category_code": d.CategoryCode,
		"condition": d.Condition, "allow_offers": d.AllowOffers, "commune_code": "00004", "photos": d.Photos, "specs": d.Specs,
	}
	l := expect[listing](t, call(t, "POST", "/v1/listings", body, bearer(seller.AccessToken), newKey()), http.StatusCreated)
	if l = settled(t, seller.AccessToken, l.ID); l.Status != "active" || len(l.Photos) != 2 || l.Photos[0].UploadID != d.Photos[0] {
		t.Fatalf("listing %+v", l)
	}
}

// A read that ends early says why and consumes the video.
func TestVideoReadFailures(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, video, failure string
		frames               int
	}{
		{"too short", shortVideo, "video_too_short", 0},
		{"no item", emptyRoom, "no_item", 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			seller, _ := newAccount(t)
			video := uploadVideo(t, seller.AccessToken, c.video)
			r := readEnd(t, seller.AccessToken, startRead(t, seller.AccessToken, video).ID)
			if r.Status != "failed" || r.Failure != c.failure || len(r.Frames) != c.frames || r.Draft != nil || r.Estimate != nil {
				t.Fatalf("%+v", r)
			}
			if s := uploadStatus(t, video); s != "consumed" {
				t.Fatalf("the video is %s", s)
			}
		})
	}
}

func TestCreateVideoReadRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	post := func(token, video string, key [2]string) Response {
		return call(t, "POST", "/v1/video-reads", map[string]any{"video": video}, bearer(token), key)
	}

	check(t, "signed out", post("", uuid.NewV7().String(), newKey()), 401, "unauthenticated")
	check(t, "no video", call(t, "POST", "/v1/video-reads", map[string]any{}, bearer(me.AccessToken), newKey()), 400, "")
	check(t, "a photo", post(me.AccessToken, upload(t, me.AccessToken, photoWithGPS(t)), newKey()), 422, "invalid_upload")
	check(t, "someone else's video", post(me.AccessToken, uploadVideo(t, other.AccessToken, "their video"), newKey()),
		422, "invalid_upload")

	video := uploadVideo(t, me.AccessToken, "a phone video of a bicycle")
	key := newKey()
	first := expect[videoRead](t, post(me.AccessToken, video, key), http.StatusAccepted)
	if again := expect[videoRead](t, post(me.AccessToken, video, key), http.StatusAccepted); again.ID != first.ID {
		t.Fatalf("a repeated key started %s after %s", again.ID, first.ID)
	}
	check(t, "read twice", post(me.AccessToken, video, newKey()), 422, "invalid_upload")

	suspended, _ := newAccount(t)
	theirs := uploadVideo(t, suspended.AccessToken, "a phone video of a bicycle")
	suspend(t, suspended.Account.ID)
	check(t, "suspended", post(suspended.AccessToken, theirs, newKey()), 403, "account_suspended")

	busy, _ := newAccount(t)
	if _, err := db.Exec(context.Background(), `INSERT INTO video_reads
    (id, account_id, video_id, status, created_at, expires_at, write_id)
SELECT gen_random_uuid(), @account, gen_random_uuid(), 'reading', now(), now() + interval '1 day', gen_random_uuid()
FROM generate_series(1, 30)`, pgx.StrictNamedArgs{"account": busy.Account.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "thirty reads", post(busy.AccessToken, uploadVideo(t, busy.AccessToken, "one more video"), newKey()),
		429, "read_limit_reached")
}

func TestGetVideoRead(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	r := readEnd(t, me.AccessToken, startRead(t, me.AccessToken, uploadVideo(t, me.AccessToken, "a phone video of a bicycle")).ID)
	path := "/v1/video-reads/" + r.ID

	check(t, "another account", call(t, "GET", path, nil, bearer(other.AccessToken)), 404, "video_read_not_found")
	check(t, "signed out", call(t, "GET", path, nil), 401, "unauthenticated")
	check(t, "not a uuid", call(t, "GET", "/v1/video-reads/nope", nil, bearer(me.AccessToken)), 400, "")
	if _, err := db.Exec(context.Background(), `UPDATE video_reads SET expires_at = now() - interval '1 minute' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": r.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "expired", call(t, "GET", path, nil, bearer(me.AccessToken)), 404, "video_read_not_found")
}

// A deleted account's reads go with it.
func TestDeletedAccountsReadsGo(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	readEnd(t, me.AccessToken, startRead(t, me.AccessToken, uploadVideo(t, me.AccessToken, "a phone video of a bicycle")).ID)
	check(t, "delete", call(t, "DELETE", "/v1/accounts/me", nil, bearer(me.AccessToken)), 204, "")
	testenv.Eventually(t, 30*time.Second, func() error {
		var n int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM video_reads WHERE account_id = @a`,
			pgx.StrictNamedArgs{"a": me.Account.ID}).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			return fmt.Errorf("%d reads left", n)
		}
		return nil
	})
}
