package service

import (
	"context"
	"errors"
	"net/url"
	"uuid"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/provider/payment"
)

// The IPN's answers, VNPay's RspCode.
var (
	answerRecorded  = NotifyAnswer{RspCode: "00", Message: "Confirm Success"}
	answerNotFound  = NotifyAnswer{RspCode: "01", Message: "Order not found"}
	answerDone      = NotifyAnswer{RspCode: "02", Message: "Order already confirmed"}
	answerAmount    = NotifyAnswer{RspCode: "04", Message: "Invalid amount"}
	answerSignature = NotifyAnswer{RspCode: "97", Message: "Invalid signature"}
	answerRetry     = NotifyAnswer{RspCode: "99", Message: "Unknown error"}
)

// Notify is UC-LST-03, step 8: the gateway reports the payment. Only a
// report whose signature checks, of a known payment and its amount, moves
// it, and only once; a success after it failed or expired still marks it
// paid (A3). E5 is an answer the gateway sends again: a fault here.
func (s *Payments) Notify(ctx context.Context, q url.Values) NotifyAnswer {
	if s.d.Gateway == nil {
		return answerNotFound
	}
	r, err := s.d.Gateway.Verify(q)
	if err != nil {
		return answerSignature
	}
	id, ok := payment.IDOf(r.Ref)
	if !ok {
		return answerNotFound
	}
	outcome, err := s.report(ctx, id, result(r), s.d.Now(), uuid.NewV7(), uuid.NewV7())
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return answerNotFound
	case errors.Is(err, domain.ErrAmountMismatch):
		s.d.Log.WarnContext(ctx, "payment report of another amount", "payment", id, "amount", r.Amount)
		return answerAmount
	case err != nil:
		s.d.Log.ErrorContext(ctx, "record payment report", "payment", id, "err", err)
		return answerRetry
	case outcome == domain.Unchanged:
		return answerDone
	default:
		return answerRecorded
	}
}
