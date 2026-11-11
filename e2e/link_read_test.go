package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

type linkRead struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Failure string `json:"failure"`
	Source  struct {
		Site       string `json:"site"`
		URL        string `json:"url"`
		PhotoCount int    `json:"photo_count"`
	} `json:"source"`
	Photos []struct {
		UploadID string `json:"upload_id"`
		URL      string `json:"url"`
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
	Estimate       *struct {
		Median int64  `json:"median"`
		Source string `json:"source"`
	} `json:"estimate"`
}

// iphoneURL is a link to listing id as the Chợ Tốt app shares it.
func iphoneURL(id int64) string {
	return fmt.Sprintf("https://www.chotot.com/tp-ho-chi-minh/quan-binh-thanh/mua-ban-dien-thoai/%d.htm?utm_source=app", id)
}

func startLinkRead(t *testing.T, token string, id int64) linkRead {
	t.Helper()
	return expect[linkRead](t, call(t, "POST", "/v1/link-reads", map[string]any{"url": iphoneURL(id)}, bearer(token), newKey()),
		http.StatusAccepted)
}

func linkReadEnd(t *testing.T, token, id string) linkRead {
	t.Helper()
	var r linkRead
	testenv.Eventually(t, 30*time.Second, func() error {
		if r = expect[linkRead](t, call(t, "GET", "/v1/link-reads/"+id, nil, bearer(token)), http.StatusOK); r.Status == "reading" {
			return fmt.Errorf("read %s still reading", id)
		}
		return nil
	})
	return r
}

func TestLinkReadToListing(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	started := startLinkRead(t, seller.AccessToken, iphoneListing)
	if started.Status != "reading" || started.Source.URL != "https://www.chotot.com/119482736.htm" || started.Draft != nil {
		t.Fatalf("started: %+v", started)
	}
	r := linkReadEnd(t, seller.AccessToken, started.ID)
	if r.Status != "done" || r.Source.PhotoCount != 3 || len(r.Photos) != 3 || r.Draft == nil || r.Estimate == nil ||
		r.ContactRemoved == nil || !*r.ContactRemoved {
		t.Fatalf("%+v", r)
	}
	d := r.Draft
	if d.Title != "iPhone 11 64GB Trắng, pin 81%" || d.CategoryCode != "phones.iphone" || d.Price == nil || *d.Price != 4_500_000 ||
		!d.AllowOffers || strings.Contains(d.Description, "0909") {
		t.Fatalf("draft %+v", d)
	}
	if r.Estimate.Source != "model" || r.Estimate.Median != 4_500_000 {
		t.Fatalf("estimate %+v", r.Estimate)
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
	if res.StatusCode != http.StatusOK {
		t.Fatalf("photo URL: %d", res.StatusCode)
	}

	body := map[string]any{
		"title": d.Title, "price": *d.Price, "description": d.Description, "category_code": d.CategoryCode,
		"condition": d.Condition, "allow_offers": d.AllowOffers, "commune_code": "00004", "photos": d.Photos, "specs": d.Specs,
	}
	l := expect[listing](t, call(t, "POST", "/v1/listings", body, bearer(seller.AccessToken), newKey()), http.StatusCreated)
	if l = settled(t, seller.AccessToken, l.ID); l.Status != "active" || len(l.Photos) != 3 {
		t.Fatalf("listing %+v", l)
	}
	seen := expect[listing](t, call(t, "GET", "/v1/listings/"+l.ID, nil, bearer(buyer.AccessToken)), http.StatusOK)
	for i, p := range seen.Photos {
		if !p.Copied || p.UploadID != d.Photos[i] {
			t.Fatalf("photo %d as a buyer sees it: %+v", i, p)
		}
	}
}

func TestLinkReadFailures(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	for _, tc := range []struct {
		id   int64
		want string
	}{
		{flatListing, "no_item"},
		{brokenListing, "listing_not_found"},
		{119400404, "listing_not_found"},
		{downListing, "unavailable"},
	} {
		if r := linkReadEnd(t, seller.AccessToken, startLinkRead(t, seller.AccessToken, tc.id).ID); r.Status != "failed" || r.Failure != tc.want {
			t.Errorf("listing %d: %+v, want %s", tc.id, r, tc.want)
		}
	}
}

func TestCreateLinkReadRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	post := func(token string, body any, key [2]string) Response {
		return call(t, "POST", "/v1/link-reads", body, bearer(token), key)
	}
	check(t, "signed out", call(t, "POST", "/v1/link-reads", map[string]any{"url": iphoneURL(iphoneListing)}, newKey()), 401, "unauthenticated")
	check(t, "no url", post(me.AccessToken, map[string]any{}, newKey()), 400, "")
	check(t, "not a link", post(me.AccessToken, map[string]any{"url": "iphone 11 trắng"}, newKey()), 422, "not_a_link")
	check(t, "facebook", post(me.AccessToken, map[string]any{"url": "https://www.facebook.com/groups/passdophcm/posts/2048817356"}, newKey()),
		422, "unsupported_source")

	key := newKey()
	first := expect[linkRead](t, post(me.AccessToken, map[string]any{"url": iphoneURL(iphoneListing)}, key), http.StatusAccepted)
	if again := expect[linkRead](t, post(me.AccessToken, map[string]any{"url": iphoneURL(iphoneListing)}, key), http.StatusAccepted); again.ID != first.ID {
		t.Fatalf("a repeated key started %s after %s", again.ID, first.ID)
	}

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "suspended", post(suspended.AccessToken, map[string]any{"url": iphoneURL(iphoneListing)}, newKey()), 403, "account_suspended")

	busy, _ := newAccount(t)
	if _, err := db.Exec(context.Background(), `INSERT INTO link_reads
    (id, account_id, site, list_id, url, status, created_at, expires_at, write_id)
SELECT gen_random_uuid(), @account, 'chotot.com', 119482736, 'https://www.chotot.com/119482736.htm', 'reading', now(),
       now() + interval '1 day', gen_random_uuid()
FROM generate_series(1, 30)`, pgx.StrictNamedArgs{"account": busy.Account.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "thirty reads", post(busy.AccessToken, map[string]any{"url": iphoneURL(iphoneListing)}, newKey()), 429, "read_limit_reached")
}

func TestGetLinkRead(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	r := linkReadEnd(t, me.AccessToken, startLinkRead(t, me.AccessToken, iphoneListing).ID)
	path := "/v1/link-reads/" + r.ID
	check(t, "another account", call(t, "GET", path, nil, bearer(other.AccessToken)), 404, "link_read_not_found")
	check(t, "signed out", call(t, "GET", path, nil), 401, "unauthenticated")
	check(t, "not a uuid", call(t, "GET", "/v1/link-reads/nope", nil, bearer(me.AccessToken)), 400, "")
	if _, err := db.Exec(context.Background(), `UPDATE link_reads SET expires_at = now() - interval '1 minute' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": r.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "expired", call(t, "GET", path, nil, bearer(me.AccessToken)), 404, "link_read_not_found")
}

func TestPreviewLink(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	preview := func(token string, body any) Response {
		return call(t, "POST", "/v1/link-previews", body, bearer(token))
	}
	p := expect[struct {
		Site       string `json:"site"`
		URL        string `json:"url"`
		Title      string `json:"title"`
		Price      *int64 `json:"price"`
		CoverURL   string `json:"cover_url"`
		PhotoCount int    `json:"photo_count"`
	}](t, preview(me.AccessToken, map[string]any{"url": iphoneURL(iphoneListing)}), http.StatusOK)
	if p.Site != "chotot.com" || p.Title != "iPhone 11 64GB trắng pin 81% zin all" || p.Price == nil || *p.Price != 4_500_000 ||
		p.PhotoCount != 3 || !strings.HasPrefix(p.CoverURL, chototStub.url()) {
		t.Fatalf("%+v", p)
	}
	check(t, "signed out", call(t, "POST", "/v1/link-previews", map[string]any{"url": iphoneURL(iphoneListing)}), 401, "unauthenticated")
	check(t, "no url", preview(me.AccessToken, map[string]any{}), 400, "")
	check(t, "not a link", preview(me.AccessToken, map[string]any{"url": "iphone"}), 422, "not_a_link")
	check(t, "nhatot", preview(me.AccessToken, map[string]any{"url": "https://nhatot.com/119482736.htm"}), 422, "unsupported_source")
	check(t, "no such listing", preview(me.AccessToken, map[string]any{"url": iphoneURL(119400404)}), 404, "listing_not_found")
	check(t, "Chợ Tốt down", preview(me.AccessToken, map[string]any{"url": iphoneURL(downListing)}), 503, "source_unavailable")
	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "suspended", preview(suspended.AccessToken, map[string]any{"url": iphoneURL(iphoneListing)}), 403, "account_suspended")
}

// A deleted account's link reads go with it.
func TestDeletedAccountsLinkReadsGo(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	linkReadEnd(t, me.AccessToken, startLinkRead(t, me.AccessToken, iphoneListing).ID)
	check(t, "delete", call(t, "DELETE", "/v1/accounts/me", nil, bearer(me.AccessToken)), 204, "")
	testenv.Eventually(t, 30*time.Second, func() error {
		var n int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM link_reads WHERE account_id = @a`,
			pgx.StrictNamedArgs{"a": me.Account.ID}).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			return fmt.Errorf("%d link reads left", n)
		}
		return nil
	})
}
