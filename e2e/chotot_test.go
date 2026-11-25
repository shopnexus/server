package e2e

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/testenv"
)

type chototCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type chototLink struct {
	OID           string  `json:"oid"`
	Name          string  `json:"name"`
	ProfileURL    string  `json:"profile_url"`
	Imported      int     `json:"imported"`
	Waiting       *int    `json:"waiting"`
	AutoImport    bool    `json:"auto_import"`
	FollowUpdates bool    `json:"follow_updates"`
	RemindHide    bool    `json:"remind_hide"`
	CheckedAt     *string `json:"checked_at"`
}

type chototImport struct {
	ListID          int64    `json:"list_id"`
	State           string   `json:"state"`
	ListingID       *string  `json:"listing_id"`
	Review          []string `json:"review"`
	MissingSpecKeys []string `json:"missing_spec_keys"`
	Reason          *string  `json:"reason"`
	Draft           *struct {
		Title       string `json:"title"`
		Price       *int64 `json:"price"`
		Description string `json:"description"`
		Condition   string `json:"condition"`
		Photos      []struct {
			UploadID string `json:"upload_id"`
		} `json:"photos"`
	} `json:"draft"`
}

type chototAd struct {
	ListID    int64         `json:"list_id"`
	State     string        `json:"state"`
	Reason    *string       `json:"reason"`
	ListingID *string       `json:"listing_id"`
	Import    *chototImport `json:"import"`
}

// linkChotot makes a profile whose bio holds a fresh code of the account
// with token, links it and returns its ID.
func linkChotot(t *testing.T, token string) string {
	t.Helper()
	code := expect[chototCode](t, call(t, "POST", "/v1/me/chotot/codes", nil, bearer(token)), http.StatusOK)
	oid := chototStub.newProfile("Shop đồ cũ uy tín. Mã: " + code.Code)
	l := expect[chototLink](t, call(t, "PUT", "/v1/me/chotot", map[string]any{"profile_url": "https://www.chotot.com/user/" + oid},
		bearer(token)), http.StatusOK)
	if l.OID != oid || l.Name != "Shop Mẫu" || !l.AutoImport || !l.FollowUpdates || !l.RemindHide || l.Waiting == nil ||
		*l.Waiting != 1 || l.CheckedAt != nil {
		t.Fatalf("link %+v", l)
	}
	return oid
}

// chototAds lists the linked profile's listings, keyed by list ID.
func chototAds(t *testing.T, token string) map[int64]chototAd {
	t.Helper()
	list := expect[struct {
		Items []chototAd `json:"items"`
		Room  int        `json:"room"`
	}](t, call(t, "GET", "/v1/me/chotot/ads", nil, bearer(token)), http.StatusOK)
	out := map[int64]chototAd{}
	for _, a := range list.Items {
		out[a.ListID] = a
	}
	return out
}

func TestChototLink(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := bearer(seller.AccessToken)
	check(t, "signed out", call(t, "POST", "/v1/me/chotot/codes", nil), 401, "")
	check(t, "signed out", call(t, "GET", "/v1/me/chotot", nil), 401, "")
	check(t, "signed out", call(t, "PUT", "/v1/me/chotot", map[string]any{"profile_url": "x"}), 401, "")
	check(t, "signed out", call(t, "PATCH", "/v1/me/chotot", map[string]any{"auto_import": false}), 401, "")
	check(t, "signed out", call(t, "DELETE", "/v1/me/chotot", nil), 401, "")
	check(t, "signed out", call(t, "GET", "/v1/me/chotot/ads", nil), 401, "")
	check(t, "signed out", call(t, "POST", "/v1/me/chotot/imports", map[string]any{"list_ids": []int64{1}}), 401, "")
	check(t, "signed out", call(t, "POST", "/v1/me/chotot/ads/1/listing", listingBody(nil)), 401, "")

	state := expect[struct {
		Link *chototLink `json:"link"`
	}](t, call(t, "GET", "/v1/me/chotot", nil, token), http.StatusOK)
	if state.Link != nil {
		t.Fatalf("a new account has a link: %+v", state.Link)
	}
	check(t, "no link", call(t, "PATCH", "/v1/me/chotot", map[string]any{"auto_import": false}, token), 404, "chotot_not_linked")
	check(t, "no link", call(t, "GET", "/v1/me/chotot/ads", nil, token), 404, "chotot_not_linked")
	check(t, "no link", call(t, "POST", "/v1/me/chotot/imports", map[string]any{"list_ids": []int64{1}}, token), 404, "chotot_not_linked")
	check(t, "no link", call(t, "POST", "/v1/me/chotot/ads/1/listing", listingBody(nil), token), 404, "chotot_not_linked")

	put := func(url string) Response {
		return call(t, "PUT", "/v1/me/chotot", map[string]any{"profile_url": url}, token)
	}
	check(t, "no body", call(t, "PUT", "/v1/me/chotot", map[string]any{}, token), 400, "")
	check(t, "a listing's address", put("https://www.chotot.com/119482736.htm"), 422, "invalid_profile_url")
	oid := chototStub.newProfile("Chưa có mã")
	check(t, "no code", put("https://www.chotot.com/user/"+oid), 422, "code_expired")
	code := expect[chototCode](t, call(t, "POST", "/v1/me/chotot/codes", nil, token), http.StatusOK)
	if again := expect[chototCode](t, call(t, "POST", "/v1/me/chotot/codes", nil, token), http.StatusOK); again != code {
		t.Fatalf("a second code while the first is live: %+v %+v", again, code)
	}
	check(t, "no such profile", put("https://www.chotot.com/user/ffffffffffffffffffffffffffffffff"), 404, "chotot_profile_not_found")
	res := put("chotot.com/user/" + oid)
	if e := errorOf(t, res); res.Status != 422 || e.Code != "code_not_found" || e.Details["profile"] == nil {
		t.Fatalf("no code in the bio: %d %s", res.Status, res.Body)
	}
	chototStub.change(oid, func(p *stubProfile) { p.down = true })
	check(t, "Chợ Tốt down", put("https://www.chotot.com/user/"+oid), 503, "chotot_unavailable")

	linked := linkChotot(t, seller.AccessToken)
	check(t, "no setting", call(t, "PATCH", "/v1/me/chotot", map[string]any{}, token), 400, "")
	l := expect[chototLink](t, call(t, "PATCH", "/v1/me/chotot", map[string]any{"remind_hide": false}, token), http.StatusOK)
	if l.OID != linked || l.RemindHide || !l.AutoImport {
		t.Fatalf("patch %+v", l)
	}
	chototStub.change(linked, func(p *stubProfile) { p.adsDown = true })
	check(t, "Chợ Tốt down", call(t, "GET", "/v1/me/chotot/ads", nil, token), 503, "chotot_unavailable")
	chototStub.change(linked, func(p *stubProfile) { p.adsDown = false })

	// Another account proves it holds the profile and takes it over.
	other, _ := newAccount(t)
	otherCode := expect[chototCode](t, call(t, "POST", "/v1/me/chotot/codes", nil, bearer(other.AccessToken)), http.StatusOK)
	chototStub.change(linked, func(p *stubProfile) { p.bio = otherCode.Code })
	expect[chototLink](t, call(t, "PUT", "/v1/me/chotot", map[string]any{"profile_url": "https://www.chotot.com/user/" + linked},
		bearer(other.AccessToken)), http.StatusOK)
	if s := expect[struct {
		Link *chototLink `json:"link"`
	}](t, call(t, "GET", "/v1/me/chotot", nil, token), http.StatusOK); s.Link != nil {
		t.Fatalf("the first account kept the profile: %+v", s.Link)
	}
	expectStatus(t, call(t, "DELETE", "/v1/me/chotot", nil, bearer(other.AccessToken)), http.StatusNoContent)
	expectStatus(t, call(t, "DELETE", "/v1/me/chotot", nil, bearer(other.AccessToken)), http.StatusNoContent)
}

// listingBody is a whole listing for POST /v1/me/chotot/ads/{list_id}/listing.
func listingBody(photos []string) map[string]any {
	if photos == nil {
		photos = []string{}
	}
	return map[string]any{
		"title": "iPhone 11 64GB Trắng", "price": 4_500_000, "description": "Máy dùng 3 năm.", "category_code": "phones.iphone",
		"condition": "used", "allow_offers": true, "commune_code": "00004", "photos": photos, "specs": iphoneSpecs,
	}
}

// importDraft links a profile for the account with token and imports its
// iPhone, which waits as a draft: the account has no commune yet.
func importDraft(t *testing.T, token string) (iphone int64, imp chototImport) {
	t.Helper()
	oid := linkChotot(t, token)
	iphone, flat := chototStub.listIDs(oid)
	ads := chototAds(t, token)
	if ads[iphone].State != "importable" || ads[flat].State != "unsupported" || *ads[flat].Reason != "real_estate" {
		t.Fatalf("ads %+v", ads)
	}
	check(t, "no list IDs", call(t, "POST", "/v1/me/chotot/imports", map[string]any{"list_ids": []int64{}}, bearer(token)), 400, "")
	started := expect[struct {
		Items []chototImport `json:"items"`
	}](t, call(t, "POST", "/v1/me/chotot/imports", map[string]any{"list_ids": []int64{iphone}}, bearer(token)), http.StatusAccepted)
	if len(started.Items) != 1 || started.Items[0].State != "importing" {
		t.Fatalf("started %+v", started)
	}
	// The listings are costly to read, so the wait reads the import's row.
	testenv.Eventually(t, time.Minute, func() error {
		var state string
		if err := db.QueryRow(context.Background(), `SELECT import_state FROM chotot_imports
WHERE account_id = (SELECT account_id FROM chotot_links WHERE oid = @oid) AND list_id = @list`,
			pgx.StrictNamedArgs{"oid": oid, "list": iphone}).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "importing" {
			return errors.New("still importing")
		}
		return nil
	})
	a := chototAds(t, token)[iphone]
	if a.Import == nil {
		t.Fatalf("no import: %+v", a)
	}
	imp = *a.Import
	if imp.State != "draft" || imp.Draft == nil || len(imp.Draft.Photos) != 3 || !slices.Contains(imp.Review, "commune_code") {
		t.Fatalf("import %+v", imp)
	}
	return iphone, imp
}

func TestChototImport(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := bearer(seller.AccessToken)
	iphone, imp := importDraft(t, seller.AccessToken)
	photos := make([]string, len(imp.Draft.Photos))
	for i, p := range imp.Draft.Photos {
		photos[i] = p.UploadID
	}
	path := "/v1/me/chotot/ads/" + strconv.FormatInt(iphone, 10) + "/listing"
	check(t, "no title", call(t, "POST", path, map[string]any{"price": 1}, token), 400, "")
	missing := listingBody(photos)
	missing["commune_code"] = ""
	check(t, "no commune", call(t, "POST", path, missing, token), 422, "invalid_listing")
	check(t, "never imported", call(t, "POST", "/v1/me/chotot/ads/"+strconv.FormatInt(iphone+5, 10)+"/listing", listingBody(photos), token), 404,
		"chotot_draft_not_found")
	listed := expect[chototImport](t, call(t, "POST", path, listingBody(photos), token), http.StatusOK)
	if listed.State != "listed" || listed.ListingID == nil || len(listed.Review) != 0 {
		t.Fatalf("listed %+v", listed)
	}
	again := expect[chototImport](t, call(t, "POST", path, listingBody(photos), token), http.StatusOK)
	if again.ListingID == nil || *again.ListingID != *listed.ListingID {
		t.Fatalf("again %+v", again)
	}
	if l := settled(t, seller.AccessToken, *listed.ListingID); l.Status != "active" || len(l.Photos) != 3 {
		t.Fatalf("the copy: %+v", l)
	}
	if a := chototAds(t, seller.AccessToken)[iphone]; a.State != "imported" || a.ListingID == nil || *a.ListingID != *listed.ListingID {
		t.Fatalf("ad %+v", a)
	}
	s := expect[struct {
		Link *chototLink `json:"link"`
	}](t, call(t, "GET", "/v1/me/chotot", nil, token), http.StatusOK)
	if s.Link == nil || s.Link.Imported != 1 {
		t.Fatalf("link %+v", s.Link)
	}
}

// A draft posts as POST /v1/listings would: a suspended seller and one at
// the cap are refused.
func TestChototDraftRefusals(t *testing.T) {
	t.Parallel()
	full, _ := newAccount(t)
	iphone, imp := importDraft(t, full.AccessToken)
	photos := []string{imp.Draft.Photos[0].UploadID}
	first := postListing(t, full.AccessToken, nil)
	if _, err := db.Exec(context.Background(), `
INSERT INTO listings (id, seller_id, title, price, description, category_code, condition, allow_offers, commune_code,
                      photos, specs, status, rejection, write_id, created_at, updated_at, lat, lng, code)
SELECT gen_random_uuid(), seller_id, title, price, description, category_code, condition, allow_offers, commune_code,
       photos, specs, 'hidden', rejection, gen_random_uuid(), created_at, updated_at, lat, lng, listing_code(gen_random_uuid(), 0)
FROM listings, generate_series(1, 19) n WHERE id = @id`, pgx.StrictNamedArgs{"id": first.ID}); err != nil {
		t.Fatal(err)
	}
	path := "/v1/me/chotot/ads/" + strconv.FormatInt(iphone, 10) + "/listing"
	check(t, "at the cap", call(t, "POST", path, listingBody(photos), bearer(full.AccessToken)), 409, "listing_cap_reached")
	suspend(t, full.Account.ID)
	check(t, "suspended", call(t, "POST", path, listingBody(photos), bearer(full.AccessToken)), 403, "account_suspended")
}
