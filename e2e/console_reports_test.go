package e2e

import (
	"net/http"
	"net/url"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	consoledomain "github.com/shopnexus/server2/internal/console/domain"
)

type reportedListing struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Price    int64  `json:"price"`
	Status   string `json:"status"`
	PhotoURL string `json:"photo_url"`
}

type reportResolution struct {
	Outcome string `json:"outcome"`
	At      string `json:"at"`
	By      struct {
		MemberID    string `json:"member_id"`
		DisplayName string `json:"display_name"`
	} `json:"by"`
}

type reportCase struct {
	Account    consoleAccount    `json:"account"`
	Reports    int               `json:"reports"`
	Reasons    map[string]int    `json:"reasons"`
	Listing    *reportedListing  `json:"listing"`
	FirstAt    string            `json:"first_at"`
	LastAt     string            `json:"last_at"`
	Resolution *reportResolution `json:"resolution"`
}

type reportCaseDetail struct {
	Account    consoleAccount    `json:"account"`
	Reasons    map[string]int    `json:"reasons"`
	Resolution *reportResolution `json:"resolution"`
	Listings   []reportedListing `json:"listings"`
	Reports    []struct {
		ID       string `json:"id"`
		Reporter struct {
			ID string `json:"id"`
		} `json:"reporter"`
		Reason    string `json:"reason"`
		Note      string `json:"note"`
		ListingID string `json:"listing_id"`
	} `json:"reports"`
}

type reportCaseList struct {
	page[reportCase]

	Total *int `json:"total"`
}

// reportCases pages through every case of status, as other tests add cases
// too, and returns where each account lands and the first page's total.
func reportCases(t *testing.T, mod consoleSession, status string) (at map[string]int, cases map[string]reportCase, total *int) {
	t.Helper()
	at, cases = map[string]int{}, map[string]reportCase{}
	cursor := ""
	for {
		q := url.Values{"status": {status}, "limit": {"100"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		list := expect[reportCaseList](t, consoleCall(t, mod, "GET", "/v1/console/reports?"+q.Encode(), nil), http.StatusOK)
		if cursor == "" {
			total = list.Total
		}
		for _, c := range list.Items {
			at[c.Account.ID], cases[c.Account.ID] = len(at), c
		}
		if list.NextCursor == "" {
			return at, cases, total
		}
		cursor = list.NextCursor
	}
}

// A case gathers an account's reports, a withdrawn one aside; dismissing
// moves it to the resolved list, a new report opens the next case, and
// reopening merges the dismissed reports back, each step audited.
func TestConsoleReportCases(t *testing.T) {
	t.Parallel()
	mod := signInMember(t, consoledomain.RoleModerator)
	admin := signInMember(t, consoledomain.RoleAdmin)
	seller, _ := newAccount(t)
	newer, _ := newAccount(t)
	b1, _ := newAccount(t)
	b2, _ := newAccount(t)
	b3, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	file := func(who signIn, body map[string]any) report {
		t.Helper()
		return expect[report](t, call(t, "POST", "/v1/reports", body, bearer(who.AccessToken)), http.StatusCreated)
	}
	first := file(b1, map[string]any{"listing_id": l.ID, "reason": "scam", "note": "Đòi cọc"})
	file(b2, map[string]any{"listing_id": l.ID, "reason": "fake"})
	file(b3, map[string]any{"account_id": seller.Account.ID, "reason": "other"})
	gone := file(b2, map[string]any{"account_id": seller.Account.ID, "reason": "scam"})
	expectStatus(t, call(t, "DELETE", "/v1/reports/"+gone.ID, nil, bearer(b2.AccessToken)), http.StatusNoContent)
	file(b1, map[string]any{"account_id": newer.Account.ID, "reason": "scam"})
	path := "/v1/console/reports/" + seller.Account.ID

	at, cases, total := reportCases(t, mod, "open")
	c, ok := cases[seller.Account.ID]
	if !ok || total == nil || *total < 2 || at[newer.Account.ID] >= at[seller.Account.ID] {
		t.Fatalf("open: seller %v at %d, newer at %d, total %v", ok, at[seller.Account.ID], at[newer.Account.ID], total)
	}
	if c.Reports != 3 || len(c.Reasons) != 3 || c.Reasons["scam"] != 1 || c.Reasons["fake"] != 1 || c.Reasons["other"] != 1 ||
		c.Listing == nil || c.Listing.ID != l.ID || c.Listing.Status != "active" || c.Listing.PhotoURL == "" || c.Resolution != nil {
		t.Fatalf("the seller's case: %+v", c)
	}
	if n := cases[newer.Account.ID]; n.Listing != nil || n.Reports != 1 {
		t.Fatalf("an account-only case: %+v", n)
	}
	d := expect[reportCaseDetail](t, consoleCall(t, mod, "GET", path, nil), http.StatusOK)
	if d.Account.ID != seller.Account.ID || len(d.Reports) != 3 || len(d.Listings) != 1 || d.Resolution != nil ||
		d.Reports[2].ID != first.ID || d.Reports[2].Reporter.ID != b1.Account.ID || d.Reports[2].Note != "Đòi cọc" ||
		d.Reports[2].ListingID != l.ID || d.Reports[0].ListingID != "" || d.Reports[0].Note != "" {
		t.Fatalf("detail: %+v", d)
	}

	d = expect[reportCaseDetail](t, consoleCall(t, mod, "POST", path+"/dismiss", nil), http.StatusOK)
	if d.Resolution == nil || d.Resolution.Outcome != "dismissed" || d.Resolution.By.MemberID != mod.ID || d.Resolution.By.DisplayName != "E2E Member" {
		t.Fatalf("dismiss: %+v", d)
	}
	check(t, "dismiss twice", consoleCall(t, mod, "POST", path+"/dismiss", nil), http.StatusConflict, "report_case_not_open")
	check(t, "resolve a dismissed case", consoleCall(t, mod, "POST", path+"/resolve", nil), http.StatusConflict, "report_case_not_open")
	if _, cases, _ = reportCases(t, mod, "open"); cases[seller.Account.ID].Reports != 0 {
		t.Fatalf("the dismissed case is still open: %+v", cases[seller.Account.ID])
	}
	resolved := expect[reportCaseList](t, consoleCall(t, mod, "GET", "/v1/console/reports?status=resolved", nil), http.StatusOK)
	if resolved.Total != nil {
		t.Fatalf("a total on the resolved list: %d", *resolved.Total)
	}
	if _, cases, _ = reportCases(t, mod, "resolved"); cases[seller.Account.ID].Resolution == nil || cases[seller.Account.ID].Reports != 3 {
		t.Fatalf("resolved: %+v", cases[seller.Account.ID])
	}
	if d = expect[reportCaseDetail](t, consoleCall(t, mod, "GET", path, nil), http.StatusOK); d.Resolution == nil || len(d.Reports) != 3 {
		t.Fatalf("the latest resolved case: %+v", d)
	}

	// b1 reports the listing again: a new row, the next case.
	again := file(b1, map[string]any{"listing_id": l.ID, "reason": "fake"})
	if again.ID == first.ID {
		t.Fatal("the new report reused the dismissed one")
	}
	if d = expect[reportCaseDetail](t, consoleCall(t, mod, "GET", path, nil), http.StatusOK); d.Resolution != nil || len(d.Reports) != 1 {
		t.Fatalf("the next case: %+v", d)
	}
	d = expect[reportCaseDetail](t, consoleCall(t, mod, "POST", path+"/reopen", nil), http.StatusOK)
	if d.Resolution != nil || len(d.Reports) != 3 || d.Reports[0].ID != again.ID {
		t.Fatalf("reopen: %+v", d)
	}
	for _, r := range d.Reports {
		if r.ID == first.ID {
			t.Fatal("b1's first report is back beside the newer one")
		}
	}
	check(t, "reopen twice", consoleCall(t, mod, "POST", path+"/reopen", nil), http.StatusConflict, "report_case_not_resolved")
	if d = expect[reportCaseDetail](t, consoleCall(t, mod, "POST", path+"/resolve", nil), http.StatusOK); d.Resolution == nil ||
		d.Resolution.Outcome != "actioned" {
		t.Fatalf("resolve: %+v", d)
	}

	type action struct {
		Action string `json:"action"`
		Target struct {
			Kind string `json:"kind"`
		} `json:"target"`
		Actor struct {
			MemberID string `json:"member_id"`
		} `json:"actor"`
		Details map[string]any `json:"details"`
	}
	q := url.Values{"target_id": {seller.Account.ID}}
	acts := expect[page[action]](t, consoleCall(t, admin, "GET", "/v1/console/actions?"+q.Encode(), nil), http.StatusOK).Items
	if len(acts) != 3 || acts[0].Action != "report.resolve" || acts[1].Action != "report.reopen" || acts[2].Action != "report.dismiss" ||
		acts[0].Target.Kind != "account" || acts[0].Actor.MemberID != mod.ID || acts[1].Details["resolution_id"] == nil {
		t.Fatalf("audit: %+v", acts)
	}

	stranger := uuid.NewV7().String()
	check(t, "never reported", consoleCall(t, mod, "GET", "/v1/console/reports/"+stranger, nil), http.StatusNotFound, "report_case_not_found")
	check(t, "a bad account ID", consoleCall(t, mod, "GET", "/v1/console/reports/x", nil), http.StatusBadRequest, "")
	check(t, "a bad status", consoleCall(t, mod, "GET", "/v1/console/reports?status=all", nil), http.StatusBadRequest, "")
	check(t, "reopen never resolved", consoleCall(t, mod, "POST", "/v1/console/reports/"+stranger+"/reopen", nil), http.StatusConflict,
		"report_case_not_resolved")
	system := "/v1/console/reports/" + accountdomain.ShopNexusID.String()
	for _, op := range []string{"/dismiss", "/resolve", "/reopen"} {
		check(t, "the system account"+op, consoleCall(t, mod, "POST", system+op, nil), http.StatusConflict, "account_is_system")
	}
	for _, c := range [][2]string{
		{"GET", "/v1/console/reports"}, {"GET", path}, {"POST", path + "/dismiss"}, {"POST", path + "/resolve"}, {"POST", path + "/reopen"},
	} {
		check(t, "signed out "+c[1], consoleCall(t, consoleSession{}, c[0], c[1], nil), http.StatusUnauthorized, "")
	}
}

// A member's hide keeps the listing off sale against its seller's unhide,
// until a member unhides it; both are audited against the listing.
func TestConsoleHidesAListing(t *testing.T) {
	t.Parallel()
	mod := signInMember(t, consoledomain.RoleModerator)
	admin := signInMember(t, consoledomain.RoleAdmin)
	seller, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	path := "/v1/console/listings/" + l.ID

	got := expect[reportedListing](t, consoleCall(t, mod, "POST", path+"/hide", nil), http.StatusOK)
	if got.ID != l.ID || got.Status != "hidden" || got.Price != l.Price || got.Title == "" {
		t.Fatalf("hide: %+v", got)
	}
	check(t, "hide twice", consoleCall(t, mod, "POST", path+"/hide", nil), http.StatusConflict, "listing_state_conflict")
	check(t, "the seller unhides", call(t, "POST", "/v1/listings/"+l.ID+"/unhide", nil, bearer(seller.AccessToken)), http.StatusConflict,
		"listing_held")
	if mine := expect[listing](t, call(t, "GET", "/v1/listings/"+l.ID, nil, bearer(seller.AccessToken)), http.StatusOK); mine.Status != "hidden" {
		t.Fatalf("the seller sees %s", mine.Status)
	}
	if got = expect[reportedListing](t, consoleCall(t, mod, "POST", path+"/unhide", nil), http.StatusOK); got.Status != "active" {
		t.Fatalf("unhide: %+v", got)
	}
	check(t, "unhide twice", consoleCall(t, mod, "POST", path+"/unhide", nil), http.StatusConflict, "listing_state_conflict")

	// The seller's own hide stays theirs: a member hides it on top, and
	// the seller still cannot unhide.
	expectStatus(t, call(t, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(seller.AccessToken)), http.StatusOK)
	expectStatus(t, consoleCall(t, mod, "POST", path+"/hide", nil), http.StatusOK)
	check(t, "the seller unhides a held listing", call(t, "POST", "/v1/listings/"+l.ID+"/unhide", nil, bearer(seller.AccessToken)),
		http.StatusConflict, "listing_held")

	type action struct {
		Action string `json:"action"`
		Target struct {
			Kind string `json:"kind"`
		} `json:"target"`
		Details map[string]any `json:"details"`
	}
	q := url.Values{"target_id": {l.ID}}
	acts := expect[page[action]](t, consoleCall(t, admin, "GET", "/v1/console/actions?"+q.Encode(), nil), http.StatusOK).Items
	if len(acts) != 3 || acts[0].Action != "listing.hide" || acts[1].Action != "listing.unhide" || acts[2].Action != "listing.hide" ||
		acts[0].Target.Kind != "listing" || acts[0].Details["account_id"] != seller.Account.ID {
		t.Fatalf("audit: %+v", acts)
	}

	unknown := "/v1/console/listings/" + uuid.NewV7().String()
	check(t, "hide an unknown listing", consoleCall(t, mod, "POST", unknown+"/hide", nil), http.StatusNotFound, "listing_not_found")
	check(t, "unhide an unknown listing", consoleCall(t, mod, "POST", unknown+"/unhide", nil), http.StatusNotFound, "listing_not_found")
	check(t, "hide signed out", consoleCall(t, consoleSession{}, "POST", path+"/hide", nil), http.StatusUnauthorized, "")
	check(t, "unhide signed out", consoleCall(t, consoleSession{}, "POST", path+"/unhide", nil), http.StatusUnauthorized, "")
}
