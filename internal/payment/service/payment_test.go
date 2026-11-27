package service_test

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/payment/repository"
	"github.com/shopnexus/server/internal/payment/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/provider/payment"
)

// gateway is the fake whose querydr answers paid for the payments in paid;
// the fake itself knows none.
type gateway struct {
	payment.Fake

	paid map[uuid.UUID]bool
}

func (g gateway) Query(_ context.Context, p payment.Lookup, _ time.Time) (payment.Result, error) {
	if !g.paid[p.ID] {
		return payment.Result{}, payment.ErrNotFound
	}
	return payment.Result{Ref: payment.Ref(p.ID), Amount: p.Amount, Paid: true, ResponseCode: "00", TransactionStatus: "00", TransactionNo: "77"}, nil
}

type env struct {
	db       *pgxpool.Pool
	payments *service.Payments
	flows    service.Flows
	client   *ingress.Client
	gateway  gateway
	account  uuid.UUID
}

func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	in, prefix := restatetest.Ingress(t), restatetest.Prefix()
	g := gateway{payment.Fake{PagesURL: "http://api.test/v1/dev/payments"}, map[uuid.UUID]bool{}}
	s := service.New(service.Deps{
		Store: repository.NewStore(db), Gateway: g, Ingress: in, Prefix: prefix, Now: time.Now, Log: slog.New(slog.DiscardHandler),
	})
	f := service.Flows{Payments: s}
	return env{db: db, payments: s, flows: f, client: restatetest.Start(t, restate.Reflect(f)), gateway: g, account: accounttest.New(t, db)}
}

func (e env) open(t *testing.T) service.View {
	t.Helper()
	p, err := e.payments.Open(context.Background(), service.OpenInput{
		Account: e.account, Product: domain.ProductBoost, ProductID: uuid.NewV7(), Amount: 15_000, IP: "203.0.113.9",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// expire runs the expiry of payment id now.
func (e env) expire(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := ingress.Service[uuid.UUID, restate.Void](e.client, e.flows.ServiceName(), "Expire").Request(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}

func (e env) events(t *testing.T, id uuid.UUID, kind string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE subject_id = $1 AND kind = $2`, id, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A payment still pending at its expiry is asked about first: one the
// gateway knows paid is recorded paid, one it does not know expires.
//
// UC-LST-03 A3.
func TestPaymentExpiry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	lost, never := e.open(t), e.open(t)
	if lost.ExpiresAt.Sub(lost.CreatedAt) != domain.ExpireAfter || lost.PayURL != "http://api.test/v1/dev/payments/"+lost.ID.String() {
		t.Fatalf("opened %+v", lost)
	}
	e.gateway.paid[lost.ID] = true
	e.expire(t, lost.ID)
	e.expire(t, never.ID)
	if p, _ := e.payments.Get(ctx, lost.ID); p.Status != domain.StatusPaid || p.GatewayRef != "77" || e.events(t, lost.ID, "payment.succeeded") != 1 {
		t.Fatalf("lost IPN %+v", p)
	}
	if p, _ := e.payments.Get(ctx, never.ID); p.Status != domain.StatusExpired || e.events(t, never.ID, "payment.expired") != 1 {
		t.Fatalf("never paid %+v", p)
	}
	// An expiry again, or of a payment never stored, changes nothing.
	e.expire(t, never.ID)
	e.expire(t, uuid.NewV7())
	if e.events(t, never.ID, "payment.expired") != 1 {
		t.Fatal("expired twice")
	}
}

// The IPN records a signed report once, of a known payment and its
// amount, and a success after the payment expired still marks it paid;
// the return page asks the gateway before it records anything.
//
// UC-LST-03 A4, A7, E9.
func TestPaymentReports(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	now := time.Now()
	p := e.open(t)
	answer := func(name string, q url.Values, code string) {
		t.Helper()
		if got := e.payments.Notify(ctx, q); got.RspCode != code {
			t.Errorf("%s: %+v, want %s", name, got, code)
		}
	}
	// E9
	answer("an unknown payment", e.gateway.Callback(uuid.NewV7(), 15_000, true, now), "01")
	answer("another amount", e.gateway.Callback(p.ID, 14_000, true, now), "04")
	// A4
	e.expire(t, p.ID)
	answer("paid after it expired", e.gateway.Callback(p.ID, 15_000, true, now), "00")
	answer("paid again", e.gateway.Callback(p.ID, 15_000, true, now), "02")
	answer("cancelled after", e.gateway.Callback(p.ID, 15_000, false, now), "02")
	if got, _ := e.payments.Get(ctx, p.ID); got.Status != domain.StatusPaid || got.ClosedAt != nil {
		t.Fatalf("after the late success %+v", got)
	}

	// A7: a return that says paid records nothing until the gateway does.
	q := e.open(t)
	back := e.gateway.Callback(q.ID, 15_000, true, now)
	if r, err := e.payments.Returned(ctx, back); err != nil || !r.Found || r.Payment.Status != domain.StatusPending {
		t.Fatalf("return before the gateway knows %+v %v", r, err)
	}
	e.gateway.paid[q.ID] = true
	if r, err := e.payments.Returned(ctx, back); err != nil || r.Payment.Status != domain.StatusPaid {
		t.Fatalf("return the gateway confirms %+v %v", r, err)
	}
	if r, _ := e.payments.Returned(ctx, url.Values{"vnp_TxnRef": {payment.Ref(q.ID)}}); r.Found {
		t.Fatal("an unsigned return found the payment")
	}
}

// The fake's page, as VNPay's at vnp_ExpireDate, takes no payment from
// its expiry on, so a late Pay records nothing.
//
// UC-LST-03 A4.
func TestDevPaymentExpires(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	p := e.open(t)
	// The dev page is served for the fake itself.
	at := func(now time.Time) *service.Payments {
		return service.New(service.Deps{
			Store: repository.NewStore(e.db), Gateway: e.gateway.Fake, Now: func() time.Time { return now }, Log: slog.New(slog.DiscardHandler),
		})
	}
	if _, err := at(p.ExpiresAt.Add(-time.Second)).DevPayment(ctx, p.ID); err != nil {
		t.Fatalf("the page in time: %v", err)
	}
	late := at(p.ExpiresAt)
	if _, err := late.DevPayment(ctx, p.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the page at expiry: %v", err)
	}
	if _, err := late.DevPay(ctx, p.ID, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("paying at expiry: %v", err)
	}
	if got, _ := e.payments.Get(ctx, p.ID); got.Status != domain.StatusPending {
		t.Fatalf("after a late press %+v", got)
	}
}
