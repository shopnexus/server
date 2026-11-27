package domain_test

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/payment/domain"
)

func TestPaymentMoves(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	fresh := func() domain.Payment {
		return domain.NewPayment(uuid.NewV7(), uuid.NewV7(), domain.ProductBoost, uuid.NewV7(), 15_000, "fake", "https://pay.test", at)
	}
	paid := domain.Result{Amount: 15_000, Paid: true, ResultCode: "00", TransactionNo: "9"}
	failed := domain.Result{Amount: 15_000, ResultCode: "24"}

	p := fresh()
	if p.State().ExpiresAt != at.Add(domain.ExpireAfter) {
		t.Fatalf("expires %v", p.State().ExpiresAt)
	}
	if _, err := p.Report(domain.Result{Amount: 1, Paid: true}, at); !errors.Is(err, domain.ErrAmountMismatch) {
		t.Fatalf("another amount: %v", err)
	}
	if o, _ := p.Report(failed, at); o != domain.Failed || p.Status() != domain.StatusFailed {
		t.Fatalf("failed %v %s", o, p.Status())
	}
	if o, _ := p.Report(failed, at); o != domain.Unchanged {
		t.Fatalf("failed again %v", o)
	}
	// Money that lands after a failure is recorded all the same.
	if o, _ := p.Report(paid, at); o != domain.Paid || p.State().ClosedAt != nil || p.State().GatewayRef != "9" {
		t.Fatalf("late success %v %+v", o, p.State())
	}
	if o, _ := p.Report(failed, at); o != domain.Unchanged || p.Expire(at) {
		t.Fatalf("a paid payment moved: %v", o)
	}
	if ok, err := p.StartRefund(); !ok || err != nil || p.Status() != domain.StatusRefunding {
		t.Fatalf("refund %v %v", ok, err)
	}
	if ok, err := p.StartRefund(); ok || err != nil {
		t.Fatalf("refund again %v %v", ok, err)
	}
	if !p.Refunded(at) || p.Refunded(at) || p.Status() != domain.StatusRefunded {
		t.Fatalf("refunded %s", p.Status())
	}

	q := fresh()
	if !q.Expire(at) || q.Expire(at) || q.Status() != domain.StatusExpired {
		t.Fatalf("expire %s", q.Status())
	}
	if _, err := q.StartRefund(); !errors.Is(err, domain.ErrNotRefundable) {
		t.Fatalf("refund unpaid: %v", err)
	}
}
