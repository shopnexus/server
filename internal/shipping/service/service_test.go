package service_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/secret"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/domain"
	"github.com/shopnexus/server/internal/shipping/repository"
	"github.com/shopnexus/server/internal/shipping/service"
)

var errNoOrder = errors.New("deal: no order has the code")

// deals plays module deal: open shipments are the set given, every
// reported code is recorded with its status, step time and fee, and a code in
// unknown has no order yet. fails moves fail that many times first, and
// with noOrder the caller has no order to price.
type deals struct {
	open     map[string]bool
	unknown  map[string]bool
	fails    int
	reported []string
	noOrder  bool
}

var errMoveFailed = errors.New("deal: the move failed")

func (d *deals) ShipmentUpdated(_ context.Context, _, code, status string, at time.Time, fee *int64) error {
	if code == "UNKNOWN" {
		return errNoOrder
	}
	if d.fails > 0 {
		d.fails--
		return errMoveFailed
	}
	r := code + " " + status + " " + at.UTC().Format("15:04")
	if fee != nil {
		r += fmt.Sprintf(" %d", *fee)
	}
	d.reported = append(d.reported, r)
	return nil
}

func (d *deals) OpenShipmentsAt(_ context.Context, _ uuid.UUID, c string) (bool, error) {
	return d.open[c], nil
}

func (d *deals) QuoteFacts(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (service.QuoteFacts, error) {
	if d.noOrder {
		return service.QuoteFacts{}, errNoOrder
	}
	return service.QuoteFacts{Recipient: carrier.Place{Name: "Hân", Commune: "Phường Bến Nghé", Province: "TP. Hồ Chí Minh"}, Price: 1_500_000}, nil
}

func (d *deals) ShipmentKnown(_ context.Context, c, code string) (bool, error) {
	return c == "ghtk" && code != "UNKNOWN" && !d.unknown[code], nil
}

type env struct {
	svc   *service.Shipping
	rows  repository.Accounts
	deals *deals
	key   *secret.Box
	owner uuid.UUID
}

func newKey(t *testing.T) *secret.Box {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(k)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func newEnv(t *testing.T, clients map[carrier.Carrier]carrier.Client) env {
	t.Helper()
	db := pgtest.New(t)
	e := env{rows: repository.NewAccounts(db), deals: &deals{open: map[string]bool{}, unknown: map[string]bool{}}, key: newKey(t), owner: accounttest.New(t, db)}
	e.svc = service.New(service.Deps{
		Accounts: e.rows, Events: repository.NewEvents(db), Clients: clients, Tokens: e.key, Deals: e.deals, DealNotFound: errNoOrder, Now: time.Now,
	})
	return e
}

func fakes() map[carrier.Carrier]carrier.Client {
	return map[carrier.Carrier]carrier.Client{carrier.GHTK: carrier.Fake{Carrier: carrier.GHTK}, carrier.GHN: carrier.Fake{Carrier: carrier.GHN}}
}

func TestLinkStoresASealedTokenAndShowsTheShopCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	v, err := e.svc.Link(ctx, e.owner, "ghtk", carrier.Credentials{Email: "an@shop.vn", Password: "x"})
	if err != nil || v.Carrier != "ghtk" || !strings.HasPrefix(v.ShopCode, "S") {
		t.Fatalf("%+v %v", v, err)
	}
	row, err := e.rows.Get(ctx, e.owner, "ghtk")
	if err != nil || strings.Contains(string(row.Token), "fake-ghtk-an@shop.vn") {
		t.Fatalf("stored %q %v", row.Token, err)
	}
	token, err := e.key.Open(row.Token, nil)
	if err != nil || string(token) != "fake-ghtk-an@shop.vn" {
		t.Fatalf("opened %q %v", token, err)
	}
	if _, err = e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t", ShopID: "885"}); err != nil {
		t.Fatal(err)
	}
	list, err := e.svc.Accounts(ctx, e.owner)
	if err != nil || len(list.Items) != 2 || list.Items[0].Carrier != "ghn" || list.Items[0].ShopCode != "885" || list.Items[1].Carrier != "ghtk" ||
		!slices.Equal(list.Linkable, []carrier.Carrier{carrier.GHN, carrier.GHTK}) {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestLinkByACodeKeepsNoToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	if err := e.svc.SendOTP(ctx, "ghn", "0356 875 408"); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		carrier, phone string
		want           error
	}{
		"GHTK":           {"ghtk", "0356875408", domain.ErrInvalidCarrier},
		"not a mobile":   {"ghn", "12345", domain.ErrPhoneUnknown},
		"no GHN account": {"ghn", "0356870000", domain.ErrPhoneUnknown},
	} {
		if err := e.svc.SendOTP(ctx, c.carrier, c.phone); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Phone: "0356875408", OTP: "wrong"}); !errors.Is(err, domain.ErrOTPInvalid) {
		t.Fatalf("a wrong code: %v", err)
	}
	v, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Phone: "0356875408", OTP: "860228"})
	if err != nil || v.ShopCode == "" {
		t.Fatalf("%+v %v", v, err)
	}
	if row, _ := e.rows.Get(ctx, e.owner, "ghn"); row.Token != nil {
		t.Fatalf("kept a token %+v", row)
	}
	// The shop is called with ShopNexus's token: an empty one.
	if _, err = e.svc.Pickups(ctx, e.owner, "ghn"); err != nil {
		t.Fatal(err)
	}
}

func TestAWrongPasswordIsLoginFailed(t *testing.T) {
	t.Parallel()
	e := newEnv(t, fakes())
	_, err := e.svc.Link(context.Background(), e.owner, "ghtk", carrier.Credentials{Email: "an@shop.vn", Password: "wrong"})
	if !errors.Is(err, domain.ErrLoginFailed) {
		t.Fatalf("%v", err)
	}
	if list, _ := e.svc.Accounts(context.Background(), e.owner); len(list.Items) != 0 {
		t.Fatalf("linked %+v", list)
	}
}

func TestUnlinkCountsOnlyThatCarrier(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	for c, cr := range map[string]carrier.Credentials{"ghtk": {Email: "a@b.vn", Password: "x"}, "ghn": {Token: "t", ShopID: "885"}} {
		if _, err := e.svc.Link(ctx, e.owner, c, cr); err != nil {
			t.Fatal(err)
		}
	}
	e.deals.open["ghn"] = true
	if err := e.svc.Unlink(ctx, e.owner, "ghn"); !errors.Is(err, domain.ErrShipmentsOpen) {
		t.Fatalf("ghn with an open shipment: %v", err)
	}
	if err := e.svc.Unlink(ctx, e.owner, "ghtk"); err != nil {
		t.Fatalf("ghtk: %v", err)
	}
	if err := e.svc.Unlink(ctx, e.owner, "ghtk"); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("ghtk again: %v", err)
	}
	if err := e.svc.Unlink(ctx, e.owner, "spx"); !errors.Is(err, domain.ErrInvalidCarrier) {
		t.Fatalf("spx: %v", err)
	}
}

// While a shipment is open at a carrier, linking there again keeps to the
// same shop: another shop's token would not know the open parcels.
func TestRelinkToAnotherShopWaitsForTheOpenShipments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	first, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t", ShopID: "885"})
	if err != nil {
		t.Fatal(err)
	}
	e.deals.open["ghn"] = true
	if _, err = e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t2", ShopID: "886"}); !errors.Is(err, domain.ErrShipmentsOpen) {
		t.Fatalf("another shop while open: %v", err)
	}
	if v, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t2", ShopID: "885"}); err != nil || v.ShopCode != first.ShopCode {
		t.Fatalf("the same shop again: %+v %v", v, err)
	}
	if row, _ := e.rows.Get(ctx, e.owner, "ghn"); row.ShopCode != "885" {
		t.Fatalf("stored %+v", row)
	}
	e.deals.open["ghn"] = false
	if v, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t2", ShopID: "886"}); err != nil || v.ShopCode != "886" {
		t.Fatalf("another shop once closed: %+v %v", v, err)
	}
}

// A shipment is made at the shop the seller links, and says so.
func TestCreateNamesTheShop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	if _, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t", ShopID: "885"}); err != nil {
		t.Fatal(err)
	}
	made, err := e.svc.Create(ctx, e.owner, carrier.GHN, carrier.CreateInput{OrderCode: "SN-1"})
	if err != nil || made.ShopCode != "885" || made.TrackingCode == "" {
		t.Fatalf("%+v %v", made, err)
	}
}

func TestNotLinkedAndNoCarrier(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	if _, err := e.svc.Lookup(ctx, e.owner, carrier.GHTK, "S1"); !errors.Is(err, domain.ErrNotLinked) {
		t.Fatalf("lookup: %v", err)
	}
	// A carrier no seller can link is one this seller has not linked.
	if _, err := e.svc.Lookup(ctx, e.owner, "spx", "SPX1"); !errors.Is(err, domain.ErrNotLinked) {
		t.Fatalf("lookup at spx: %v", err)
	}
	if _, err := e.svc.Pickups(ctx, e.owner, "ghn"); !errors.Is(err, domain.ErrNotLinked) {
		t.Fatalf("pickups: %v", err)
	}
	if _, err := e.svc.Link(ctx, e.owner, "spx", carrier.Credentials{}); !errors.Is(err, domain.ErrInvalidCarrier) {
		t.Fatalf("spx: %v", err)
	}
	none := newEnv(t, map[carrier.Carrier]carrier.Client{})
	for name, err := range map[string]error{
		"link":   func() error { _, err := none.svc.Link(ctx, none.owner, "ghtk", carrier.Credentials{}); return err }(),
		"lookup": func() error { _, err := none.svc.Lookup(ctx, none.owner, carrier.GHTK, "S1"); return err }(),
		"create": func() error {
			_, err := none.svc.Create(ctx, none.owner, carrier.GHN, carrier.CreateInput{})
			return err
		}(),
		"pickups": func() error { _, err := none.svc.Pickups(ctx, none.owner, "ghtk"); return err }(),
		"quote": func() error {
			_, err := none.svc.Quote(ctx, none.owner, "ghtk", service.QuoteRequest{Service: "x"})
			return err
		}(),
	} {
		if !errors.Is(err, domain.ErrUnavailable) {
			t.Errorf("%s with no carrier: %v", name, err)
		}
	}
}

// Each carrier is on or off by itself: with GHN alone a seller links GHN,
// GHTK answers carrier_unavailable, an account kept at GHTK can still be
// dropped, and a code with no client stays invalid_carrier.
func TestOneCarrierOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, map[carrier.Carrier]carrier.Client{carrier.GHN: carrier.Fake{Carrier: carrier.GHN}})
	if _, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t", ShopID: "885"}); err != nil {
		t.Fatalf("ghn: %v", err)
	}
	if _, err := e.svc.Link(ctx, e.owner, "ghtk", carrier.Credentials{Email: "a@b.vn", Password: "x"}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("ghtk off: %v", err)
	}
	if _, err := e.svc.Link(ctx, e.owner, "spx", carrier.Credentials{}); !errors.Is(err, domain.ErrInvalidCarrier) {
		t.Fatalf("spx: %v", err)
	}
	if err := e.rows.Put(ctx, repository.Row{AccountID: e.owner, Carrier: "ghtk", ShopCode: "S1", Token: e.key.Seal([]byte("t"), nil), LinkedAt: time.Now()}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(ctx, e.owner, carrier.GHTK, carrier.CreateInput{}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("a shipment at ghtk off: %v", err)
	}
	if list, err := e.svc.Accounts(ctx, e.owner); err != nil || len(list.Items) != 2 || !slices.Equal(list.Linkable, []carrier.Carrier{carrier.GHN}) {
		t.Fatalf("ghtk off is not linkable: %+v %v", list, err)
	}
	if err := e.svc.Unlink(ctx, e.owner, "ghtk"); err != nil {
		t.Fatalf("unlink ghtk off: %v", err)
	}
}

// The dev endpoint moves a fake carrier's parcels only, never a live one's.
func TestDevReportedSkipsALiveCarrier(t *testing.T) {
	t.Parallel()
	e := newEnv(t, map[carrier.Carrier]carrier.Client{carrier.GHTK: refuser{carrier.Fake{Carrier: carrier.GHTK}}})
	err := e.svc.DevReported(context.Background(), "S1", carrier.Event{Kind: carrier.KindStep, Step: carrier.StepPicked})
	if !errors.Is(err, errNoOrder) || len(e.deals.reported) != 0 {
		t.Fatalf("%v %v", err, e.deals.reported)
	}
}

func TestQuoteChecksTheShipmentThenAsksTheCarrier(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	req := service.QuoteRequest{OrderID: uuid.NewV7(), Service: "standard", WeightGrams: 800, PickupPointID: "88256"}
	if _, err := e.svc.Quote(ctx, e.owner, "ghtk", req); !errors.Is(err, domain.ErrNotLinked) {
		t.Fatalf("not linked: %v", err)
	}
	// Someone else's order is not found before the carrier is asked about.
	theirs := newEnv(t, fakes())
	theirs.svc = service.New(service.Deps{Accounts: theirs.rows, Clients: fakes(), Tokens: theirs.key, Now: time.Now, Deals: &deals{noOrder: true}})
	if _, err := theirs.svc.Quote(ctx, theirs.owner, "ghtk", req); !errors.Is(err, errNoOrder) {
		t.Fatalf("someone else's order: %v", err)
	}
	if _, err := e.svc.Link(ctx, e.owner, "ghtk", carrier.Credentials{Email: "a@b.vn", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	if fee, err := e.svc.Quote(ctx, e.owner, "ghtk", req); err != nil || fee != 22_000 {
		t.Fatalf("%d %v", fee, err)
	}
	for name, bad := range map[string]service.QuoteRequest{
		"a service":         {OrderID: req.OrderID, Service: "rocket", WeightGrams: 800},
		"no weight":         {OrderID: req.OrderID, Service: "standard"},
		"a point and an id": {OrderID: req.OrderID, Service: "standard", WeightGrams: 800, PickupPointID: "1", AddressID: uuid.NewV7()},
	} {
		if _, err := e.svc.Quote(ctx, e.owner, "ghtk", bad); !errors.Is(err, domain.ErrInvalidShipment) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A report keeps its events once and sends the order the status of the
// parcel's newest step with its time, a repeat or a late step too, which
// deal ignores. A failed delivery stays in transit. A code no order has,
// or another carrier's code, keeps its events and moves nothing.
//
// UC-DEAL-05.
func TestReportedKeepsTheTimelineAndMovesToTheNewestStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	code := "S1." + uuid.NewV7().String()
	ghtk := func(id int, at, extra string) string {
		return fmt.Sprintf(`{"label_id":%q,"status_id":%d,"action_time":%q%s}`, code, id, at, extra)
	}
	for _, r := range []struct {
		c    carrier.Carrier
		body string
	}{
		{carrier.GHTK, ghtk(3, "2026-11-24T09:00:00Z", `,"weight":0.8`)},
		{carrier.GHTK, ghtk(3, "2026-11-24T09:00:00Z", `,"weight":0.8`)},
		{carrier.GHTK, ghtk(2, "2026-11-24T08:00:00Z", "")},
		{carrier.GHTK, ghtk(9, "2026-11-24T10:00:00Z", `,"reason":"Không liên lạc được"`)},
		{carrier.GHN, fmt.Sprintf(`{"OrderCode":%q,"Status":"returned"}`, code)},
		{carrier.GHTK, `{"label_id":"UNKNOWN","status_id":3}`},
	} {
		if err := e.svc.Reported(ctx, r.c, "application/json", []byte(r.body)); err != nil {
			t.Fatalf("%s: %v", r.body, err)
		}
	}
	if err := e.svc.Reported(ctx, carrier.GHTK, "application/json", []byte("not json")); !errors.Is(err, domain.ErrReportUnread) {
		t.Fatalf("an unreadable report: %v", err)
	}
	want := []string{code + " in_transit 09:00", code + " in_transit 09:00", code + " in_transit 09:00", code + " in_transit 10:00"}
	if !slices.Equal(e.deals.reported, want) {
		t.Fatalf("%v", e.deals.reported)
	}
	for c, n := range map[string]int{"ghn": 1} {
		if kept, _ := e.svc.Tracking(ctx, c, code, true); len(kept.Events) != n {
			t.Errorf("%s keeps %+v", c, kept)
		}
	}
	if kept, _ := e.svc.Tracking(ctx, "ghtk", "UNKNOWN", true); len(kept.Events) != 1 {
		t.Errorf("a code no order has keeps %+v", kept)
	}
	all, err := e.svc.Tracking(ctx, "ghtk", code, true)
	if err != nil || all.Step != carrier.StepDeliveryFailed || len(all.Events) != 4 ||
		all.Events[0].Reason != "Không liên lạc được" || all.Events[3].Step != carrier.StepReadyToPick {
		t.Fatalf("%+v %v", all, err)
	}
	if b, _ := e.svc.Tracking(ctx, "ghtk", code, false); len(b.Events) != 3 || b.Step != all.Step {
		t.Fatalf("the buyer sees the weight: %+v", b)
	}
	if err = e.svc.DevReported(ctx, "UNKNOWN", carrier.Event{At: time.Now(), Kind: carrier.KindStep, Step: carrier.StepPicked}); !errors.Is(err, errNoOrder) {
		t.Fatalf("a code no order has: %v", err)
	}
}

// A step the webhook brings before DealShip saved its code is kept, and
// the next report moves the order to it. A move that failed is sent
// again when the carrier resends the report, though nothing new is kept.
//
// UC-DEAL-05.
func TestReportedMovesAfterAnEarlyOrFailedReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	code := "S1." + uuid.NewV7().String()
	picked := fmt.Sprintf(`{"label_id":%q,"status_id":3,"action_time":"2026-11-24T09:00:00Z"}`, code)
	fee := fmt.Sprintf(`{"label_id":%q,"status_id":123,"fee":30000,"action_time":"2026-11-24T09:30:00Z"}`, code)
	e.deals.unknown[code] = true
	if err := e.svc.Reported(ctx, carrier.GHTK, "application/json", []byte(picked)); err != nil {
		t.Fatal(err)
	}
	if len(e.deals.reported) != 0 {
		t.Fatalf("moved before the order had the code: %v", e.deals.reported)
	}
	delete(e.deals.unknown, code)
	e.deals.fails = 1
	if err := e.svc.Reported(ctx, carrier.GHTK, "application/json", []byte(fee)); !errors.Is(err, errMoveFailed) {
		t.Fatalf("a failed move: %v", err)
	}
	if err := e.svc.Reported(ctx, carrier.GHTK, "application/json", []byte(fee)); err != nil {
		t.Fatal(err)
	}
	if want := []string{code + " in_transit 09:00 30000"}; !slices.Equal(e.deals.reported, want) {
		t.Fatalf("%v", e.deals.reported)
	}
}

// Track keeps the carrier's fee as a fee event, once while it stays the
// same, and answers the timeline's newest fee.
func TestTrackKeepsTheFee(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	if _, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t", ShopID: "885"}); err != nil {
		t.Fatal(err)
	}
	code := "GHN-" + uuid.NewV7().String()
	for range 2 {
		p, err := e.svc.Track(ctx, e.owner, carrier.GHN, code)
		if err != nil || p.Fee == nil || *p.Fee != 22_000 {
			t.Fatalf("%+v %v", p, err)
		}
	}
	kept, err := e.svc.Tracking(ctx, "ghn", code, true)
	if err != nil || len(kept.Events) != 1 || kept.Events[0].Kind != carrier.KindFee || kept.Fee == nil || *kept.Fee != 22_000 {
		t.Fatalf("%+v %v", kept, err)
	}
	if b, _ := e.svc.Tracking(ctx, "ghn", code, false); len(b.Events) != 0 || b.Fee != nil {
		t.Fatalf("the buyer sees the fee: %+v", b)
	}
}

func TestAccountDeletedDropsTheTokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	if _, err := e.svc.Link(ctx, e.owner, "ghn", carrier.Credentials{Token: "t", ShopID: "885"}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.AccountDeleted(ctx, e.owner); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.svc.Accounts(ctx, e.owner); len(list.Items) != 0 {
		t.Fatalf("%+v", list)
	}
}

func TestAWrongKeyTokenIsAnErrorWithoutTheToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, fakes())
	sealed := newKey(t).Seal([]byte("token-of-another-stack"), nil)
	if err := e.rows.Put(ctx, repository.Row{AccountID: e.owner, Carrier: "ghtk", ShopCode: "S1", Token: sealed, LinkedAt: time.Now()}, nil); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.Lookup(ctx, e.owner, carrier.GHTK, "S1.A1")
	if !errors.Is(err, domain.ErrRelinkNeeded) || !errors.Is(err, secret.ErrOpen) || strings.Contains(err.Error(), "token-of-another-stack") {
		t.Fatalf("%v", err)
	}
	// Linking again replaces the token that does not open.
	if _, err = e.svc.Link(ctx, e.owner, "ghtk", carrier.Credentials{Email: "a@b.vn", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.Lookup(ctx, e.owner, carrier.GHTK, "S1.A1"); err != nil {
		t.Fatalf("after relinking: %v", err)
	}
}

// refuser is a carrier that refuses every shipment, as GHTK does for a bad
// phone number.
type refuser struct{ carrier.Fake }

func (refuser) Create(context.Context, carrier.Account, carrier.CreateInput) (carrier.Created, error) {
	return carrier.Created{}, fmt.Errorf("%w: Số điện thoại người nhận không hợp lệ", carrier.ErrRefused)
}

// A carrier's refusal is the seller's to fix, not a failure to try again.
func TestACarriersRefusalIsCarrierRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, map[carrier.Carrier]carrier.Client{carrier.GHTK: refuser{carrier.Fake{Carrier: carrier.GHTK}}})
	if _, err := e.svc.Link(ctx, e.owner, "ghtk", carrier.Credentials{Email: "a@b.vn", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.Create(ctx, e.owner, carrier.GHTK, carrier.CreateInput{OrderCode: "SN-1"})
	if !errors.Is(err, domain.ErrCarrierRefused) || !strings.Contains(err.Error(), "không hợp lệ") {
		t.Fatalf("%v", err)
	}
}
