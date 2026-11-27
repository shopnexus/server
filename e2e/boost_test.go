package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/app"
	consoledomain "github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/platform/testenv"
)

type boostView struct {
	ID               string     `json:"id"`
	ListingID        string     `json:"listing_id"`
	Package          string     `json:"package"`
	Days             int        `json:"days"`
	Price            int64      `json:"price"`
	Status           string     `json:"status"`
	PaymentURL       string     `json:"payment_url"`
	PaymentExpiresAt *time.Time `json:"payment_expires_at"`
	StartedAt        *time.Time `json:"started_at"`
	EndsAt           *time.Time `json:"ends_at"`
	NextBumpAt       *time.Time `json:"next_bump_at"`
	Bumps            int        `json:"bumps"`
}

// buy buys pkg for listing as token under key and expects status.
func buy(t *testing.T, token, listing, pkg string, key [2]string, status int) boostView {
	t.Helper()
	return expect[boostView](t, call(t, "POST", "/v1/boosts", map[string]any{"listing_id": listing, "package": pkg}, bearer(token), key), status)
}

// devPath is the fake gateway's page of a boost's payment, as a path on
// the shared server.
func devPath(t *testing.T, b boostView) string {
	t.Helper()
	u, err := url.Parse(b.PaymentURL)
	if err != nil || !strings.HasPrefix(u.Path, "/v1/dev/payments/") {
		t.Fatalf("payment_url %q", b.PaymentURL)
	}
	return u.Path
}

// pay presses Pay or Cancel on the fake gateway's page and follows the
// browser back to the return page, which opens the app at the boost.
func pay(t *testing.T, b boostView, result string) {
	t.Helper()
	res := call(t, "POST", devPath(t, b), form{"result": {result}})
	expectStatus(t, res, http.StatusSeeOther)
	back := res.Header.Get("Location")
	if !strings.HasPrefix(back, "/v1/payments/vnpay/return?") {
		t.Fatalf("Location %q", back)
	}
	page := call(t, "GET", back, nil)
	if page.Status != http.StatusOK || !strings.Contains(string(page.Body), "shopnexus://boost/"+b.ID) {
		t.Fatalf("return page: %d %s", page.Status, page.Body)
	}
}

// boostIn polls token's boost until its status is status.
func boostIn(t *testing.T, token, id, status string) boostView {
	t.Helper()
	var b boostView
	testenv.Eventually(t, 30*time.Second, func() error {
		if b = expect[boostView](t, call(t, "GET", "/v1/boosts/"+id, nil, bearer(token)), http.StatusOK); b.Status != status {
			return fmt.Errorf("boost %s is %s, want %s", id, b.Status, status)
		}
		return nil
	})
	return b
}

// offered reads whether the seller's listing view and Tin của bạn offer to
// boost listing, and the boost Tin của bạn shows on it.
func offered(t *testing.T, token, listing string) (onView *bool, onMine bool, mine *boostView) {
	t.Helper()
	v := expect[struct {
		Boostable *bool `json:"boostable"`
	}](t, call(t, "GET", "/v1/screens/listing/"+listing, nil, bearer(token)), http.StatusOK)
	l := expect[struct {
		Items []struct {
			Listing struct {
				ID string `json:"id"`
			} `json:"listing"`
			Boost     *boostView `json:"boost"`
			Boostable bool       `json:"boostable"`
		} `json:"items"`
	}](t, call(t, "GET", "/v1/screens/my-listings?pane=on_sale", nil, bearer(token)), http.StatusOK)
	for i := range l.Items {
		if l.Items[i].Listing.ID == listing {
			return v.Boostable, l.Items[i].Boostable, l.Items[i].Boost
		}
	}
	t.Fatalf("listing %s is not in Tin của bạn", listing)
	return nil, false, nil
}

// sellerOrder is the IDs of the seller's public list, newest first.
func sellerOrder(t *testing.T, seller string) []string {
	t.Helper()
	l := expect[page[searchCard]](t, call(t, "GET", "/v1/sellers/"+seller+"/listings", nil), http.StatusOK)
	ids := make([]string, len(l.Items))
	for i := range l.Items {
		ids[i] = l.Items[i].ID
	}
	return ids
}

// A seller buys the 3-day package and pays at the fake gateway: the
// listing moves to the top of the newest order with its posted time kept,
// the seller hears of it, and a second purchase is refused while the
// package runs. The IPN records a payment once and checks its signature.
//
// UC-LST-03 A1, A2, E7.
func TestBoostAListing(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := seller.AccessToken
	older := activeListing(t, token, nil)
	newer := activeListing(t, token, nil)
	if got := sellerOrder(t, seller.Account.ID); len(got) != 2 || got[0] != newer.ID {
		t.Fatalf("before: %v", got)
	}

	packages := expect[struct {
		Items []struct {
			ID    string `json:"id"`
			Days  int    `json:"days"`
			Price int64  `json:"price"`
		} `json:"items"`
		Available bool `json:"available"`
	}](t, call(t, "GET", "/v1/boost-packages", nil), http.StatusOK)
	if !packages.Available || len(packages.Items) != 2 || packages.Items[0].ID != "3d" || packages.Items[0].Price != 15_000 {
		t.Fatalf("packages %+v", packages)
	}

	if onView, onMine, mine := offered(t, token, older.ID); onView == nil || !*onView || !onMine || mine != nil {
		t.Fatalf("before: view %v, mine %v %+v", onView, onMine, mine)
	}
	stranger, _ := newAccount(t)
	if v := expect[struct {
		Boostable *bool `json:"boostable"`
	}](t, call(t, "GET", "/v1/screens/listing/"+older.ID, nil, bearer(stranger.AccessToken)), http.StatusOK); v.Boostable != nil {
		t.Fatal("boostable shown to another account")
	}

	key := newKey()
	b := buy(t, token, older.ID, "3d", key, http.StatusCreated)
	if b.Status != "awaiting_payment" || b.Days != 3 || b.Price != 15_000 || b.PaymentExpiresAt == nil {
		t.Fatalf("bought %+v", b)
	}
	if onView, onMine, mine := offered(t, token, older.ID); onView == nil || *onView || onMine || mine == nil || mine.ID != b.ID {
		t.Fatalf("awaiting payment: view %v, mine %v %+v", onView, onMine, mine)
	}
	// A1: the same key; A2: another purchase of the same package.
	if again := buy(t, token, older.ID, "3d", key, http.StatusOK); again.ID != b.ID {
		t.Fatalf("same key %+v", again)
	}
	if again := buy(t, token, older.ID, "3d", newKey(), http.StatusOK); again.ID != b.ID || again.PaymentURL != b.PaymentURL {
		t.Fatalf("same package %+v", again)
	}
	page := call(t, "GET", devPath(t, b), nil)
	if page.Status != http.StatusOK || !strings.Contains(string(page.Body), "15.000") {
		t.Fatalf("dev page: %d %s", page.Status, page.Body)
	}
	pay(t, b, "pay")

	started := boostIn(t, token, b.ID, "active")
	if started.Bumps != 1 || started.StartedAt == nil || started.EndsAt == nil || started.NextBumpAt == nil ||
		!started.EndsAt.Equal(started.StartedAt.Add(72*time.Hour)) || !started.NextBumpAt.Equal(started.StartedAt.Add(24*time.Hour)) ||
		started.PaymentURL != "" {
		t.Fatalf("started %+v", started)
	}
	if got := sellerOrder(t, seller.Account.ID); len(got) != 2 || got[0] != older.ID {
		t.Fatalf("after: %v", got)
	}
	l := expect[struct {
		PublishedAt time.Time  `json:"published_at"`
		BumpedAt    *time.Time `json:"bumped_at"`
	}](t, call(t, "GET", "/v1/listings/"+older.ID, nil, bearer(token)), http.StatusOK)
	if l.BumpedAt == nil || !l.BumpedAt.After(l.PublishedAt) {
		t.Fatalf("listing times %+v", l)
	}
	if n := newestNotice(t, token, "boost_started").Items[0]; n.Target.Kind != "listing" || n.Target.ID != older.ID {
		t.Fatalf("notice %+v", n)
	}

	// E7: one package at a time.
	res := call(t, "POST", "/v1/boosts", map[string]any{"listing_id": older.ID, "package": "7d"}, bearer(token), newKey())
	if e := errorOf(t, res); res.Status != http.StatusConflict || e.Code != "boost_active" || e.Details["boost_id"] != b.ID {
		t.Fatalf("while running: %d %s", res.Status, res.Body)
	}
	// The page of a paid payment is gone.
	check(t, "the paid payment's page", call(t, "GET", devPath(t, b), nil), http.StatusNotFound, "payment_not_found")
	check(t, "paying again", call(t, "POST", devPath(t, b), form{"result": {"pay"}}), http.StatusNotFound, "payment_not_found")
}

// The IPN answers VNPay's codes: a report it recorded already, a bad
// signature, an unknown payment. A return page with a bad signature says
// it found nothing.
//
// UC-LST-03 E9.
func TestPaymentNotify(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	b := buy(t, seller.AccessToken, l.ID, "3d", newKey(), http.StatusCreated)
	res := call(t, "POST", devPath(t, b), form{"result": {"pay"}})
	expectStatus(t, res, http.StatusSeeOther)
	signed := strings.TrimPrefix(res.Header.Get("Location"), "/v1/payments/vnpay/return?")
	answer := func(name, query, code string) {
		t.Helper()
		got := expect[struct {
			RspCode string `json:"RspCode"` //nolint:tagliatelle // VNPay's
		}](t, call(t, "GET", "/v1/payments/vnpay/ipn?"+query, nil), http.StatusOK)
		if got.RspCode != code {
			t.Errorf("%s: %s, want %s", name, got.RspCode, code)
		}
	}
	answer("a report again", signed, "02")
	q, _ := url.ParseQuery(signed)
	q.Set("vnp_Amount", "100")
	answer("another amount, unsigned", q.Encode(), "97")
	answer("no signature", "vnp_TxnRef=abc", "97")
	page := call(t, "GET", "/v1/payments/vnpay/return?"+q.Encode(), nil)
	if page.Status != http.StatusOK || strings.Contains(string(page.Body), "shopnexus://boost/") {
		t.Fatalf("bad return page: %d %s", page.Status, page.Body)
	}
}

// A seller who cancels at the gateway leaves the boost failed and hears of
// it. While one package awaits payment another is refused, naming it; a
// payment that lands after its listing left sale still starts the package,
// which moves nothing until the listing is back on sale.
//
// UC-LST-03 A5, E8, E10.
func TestBoostPaymentClosesOrStarts(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	token := seller.AccessToken
	l := activeListing(t, token, nil)

	// E8
	cancelled := buy(t, token, l.ID, "3d", newKey(), http.StatusCreated)
	pay(t, cancelled, "cancel")
	boostIn(t, token, cancelled.ID, "failed")
	newestNotice(t, token, "boost_failed")

	// E10
	pending := buy(t, token, l.ID, "7d", newKey(), http.StatusCreated)
	res := call(t, "POST", "/v1/boosts", map[string]any{"listing_id": l.ID, "package": "3d"}, bearer(token), newKey())
	if e := errorOf(t, res); res.Status != http.StatusConflict || e.Code != "boost_pending" || e.Details["boost_id"] != pending.ID {
		t.Fatalf("another package while one awaits payment: %d %s", res.Status, res.Body)
	}

	// A5
	expectStatus(t, call(t, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(token)), http.StatusOK)
	pay(t, pending, "pay")
	b := boostIn(t, token, pending.ID, "active")
	if b.StartedAt == nil || b.Bumps != 0 || b.EndsAt == nil || !b.EndsAt.Equal(b.StartedAt.Add(7*24*time.Hour)) {
		t.Fatalf("started off sale %+v", b)
	}
	newestNotice(t, token, "boost_started")
}

// The checks before a purchase, in the spec's order.
//
// UC-LST-03 E2, E3, E4, E5, E6.
func TestBoostRefusals(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	other, _ := newAccount(t)
	token := seller.AccessToken
	l := activeListing(t, token, nil)
	hidden := activeHidden(t, token)
	body := func(listing, pkg string) map[string]any { return map[string]any{"listing_id": listing, "package": pkg} }

	check(t, "signed out", call(t, "POST", "/v1/boosts", body(l.ID, "3d"), newKey()), http.StatusUnauthorized, "")
	check(t, "malformed", call(t, "POST", "/v1/boosts", raw{"application/json", "{"}, bearer(token), newKey()), http.StatusBadRequest, "")
	check(t, "E2 an unknown package", call(t, "POST", "/v1/boosts", body(l.ID, "1d"), bearer(token), newKey()),
		http.StatusUnprocessableEntity, "unknown_boost_package")
	check(t, "E5 another seller's", call(t, "POST", "/v1/boosts", body(l.ID, "3d"), bearer(other.AccessToken), newKey()),
		http.StatusNotFound, "listing_not_found")
	check(t, "E5 no such listing", call(t, "POST", "/v1/boosts", body(uuid.NewV7().String(), "3d"), bearer(token), newKey()),
		http.StatusNotFound, "listing_not_found")
	check(t, "E6 a hidden listing", call(t, "POST", "/v1/boosts", body(hidden, "3d"), bearer(token), newKey()),
		http.StatusConflict, "listing_not_boostable")
	for _, it := range myListings(t, token, "pane=on_sale").Items {
		if it.Listing.ID == hidden && it.Boostable {
			t.Fatal("a hidden listing is boostable")
		}
	}
	emailOnly, _ := newEmailAccount(t)
	check(t, "E4 no phone", call(t, "POST", "/v1/boosts", body(l.ID, "3d"), bearer(emailOnly.AccessToken), newKey()),
		http.StatusForbidden, "phone_required")
	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "E3 suspended", call(t, "POST", "/v1/boosts", body(l.ID, "3d"), bearer(suspended.AccessToken), newKey()),
		http.StatusForbidden, "account_suspended")

	b := buy(t, token, l.ID, "3d", newKey(), http.StatusCreated)
	check(t, "another's boost", call(t, "GET", "/v1/boosts/"+b.ID, nil, bearer(other.AccessToken)), http.StatusNotFound, "boost_not_found")
	check(t, "a boost signed out", call(t, "GET", "/v1/boosts/"+b.ID, nil), http.StatusUnauthorized, "")
	check(t, "no such payment page", call(t, "GET", "/v1/dev/payments/"+uuid.NewV7().String(), nil), http.StatusNotFound, "payment_not_found")
	check(t, "a bad press", call(t, "POST", devPath(t, b), form{"result": {"maybe"}}), http.StatusBadRequest, "")

	// The console reads the listing's boosts with their payments.
	mod := signInMember(t, consoledomain.RoleModerator)
	list := expect[page[struct {
		Boost   boostView `json:"boost"`
		Payment struct {
			Status  string `json:"status"`
			Gateway string `json:"gateway"`
			Amount  int64  `json:"amount"`
		} `json:"payment"`
	}]](t, consoleCall(t, mod, "GET", "/v1/console/listings/"+l.ID+"/boosts", nil), http.StatusOK)
	if len(list.Items) != 1 || list.Items[0].Boost.ID != b.ID || list.Items[0].Payment.Status != "pending" ||
		list.Items[0].Payment.Gateway != "fake" || list.Items[0].Payment.Amount != 15_000 {
		t.Fatalf("console %+v", list)
	}
	check(t, "the console signed out", consoleCall(t, consoleSession{}, "GET", "/v1/console/listings/"+l.ID+"/boosts", nil),
		http.StatusUnauthorized, "")
}

// A stack that takes no payments, as production does until VNPay is
// live, lists the packages unavailable and refuses a purchase.
//
// UC-LST-03 E1.
func TestBoostWithoutPayments(t *testing.T) {
	t.Parallel()
	cfg := serverConfig
	store := pgtest.New(t)
	cfg.DatabaseURL, cfg.Restate = store.Config().ConnString(), restatetest.Config(t)
	cfg.Payment.Mode, cfg.Gateway.URL = "off", ownGateway(t)
	a, err := app.New(context.Background(), cfg, discard,
		app.WithPushSender(phones), app.WithEmailSender(mails), app.WithPasswordHasher(consoledomain.Fast), fakeAI())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	me, _ := newAccount(t)
	if p := expect[struct{ Available bool }](t, callAt(t, srv.URL, "GET", "/v1/boost-packages", nil), http.StatusOK); p.Available {
		t.Fatal("packages available without payments")
	}
	check(t, "a purchase", callAt(t, srv.URL, "POST", "/v1/boosts", map[string]any{"listing_id": uuid.NewV7().String(), "package": "3d"},
		bearer(me.AccessToken), newKey()), http.StatusServiceUnavailable, "payment_unavailable")
}
