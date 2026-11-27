package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/payment/repository"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestPaymentInsertOncePerProductAndSave(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	store := repository.NewStore(db)
	account := accounttest.New(t, db)
	at := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	boost := uuid.NewV7()
	first := domain.NewPayment(uuid.NewV7(), account, domain.ProductBoost, boost, 15_000, "fake", "https://pay.test/1", at)
	got, err := store.Insert(ctx, first)
	if err != nil || got.ID() != first.ID() || got.Status() != domain.StatusPending {
		t.Fatalf("insert %+v %v", got.State(), err)
	}
	// A second payment for the same boost returns the first.
	again, err := store.Insert(ctx, domain.NewPayment(uuid.NewV7(), account, domain.ProductBoost, boost, 15_000, "fake", "https://pay.test/2", at))
	if err != nil || again.ID() != first.ID() || again.State().PayURL != "https://pay.test/1" {
		t.Fatalf("second insert %+v %v", again.State(), err)
	}
	if _, err = store.Get(ctx, uuid.NewV7()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown payment: %v", err)
	}

	stale := got
	if _, err = got.Report(domain.Result{Amount: 15_000, Paid: true, ResultCode: "00", TransactionNo: "123", BankCode: "NCB"}, at); err != nil {
		t.Fatal(err)
	}
	ev := events.Event{ID: uuid.NewV7(), Kind: events.PaymentSucceeded, SubjectID: got.ID(), OccurredAt: at}
	if err = store.Save(ctx, &got, uuid.NewV7(), ev); err != nil {
		t.Fatal(err)
	}
	stale.Expire(at)
	if err = store.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	many, err := store.GetMany(ctx, []uuid.UUID{got.ID(), uuid.NewV7()})
	if err != nil || len(many) != 1 {
		t.Fatalf("get many %v %v", many, err)
	}
	saved := many[got.ID()]
	st := saved.State()
	if st.Status != domain.StatusPaid || st.GatewayRef != "123" || st.BankCode != "NCB" || st.PaidAt == nil || !st.PaidAt.Equal(at) {
		t.Fatalf("saved %+v", st)
	}
	var n int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM events WHERE subject_id = $1 AND kind = 'payment.succeeded'`, got.ID()).Scan(&n); err != nil || n != 1 {
		t.Fatalf("events %d %v", n, err)
	}
}
