package orderbiz

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
)

// mapPaymentSession converts an sqlc OrderPaymentSession row to the domain model.
func mapPaymentSession(s orderdb.OrderPaymentSession) ordermodel.PaymentSession {
	return ordermodel.PaymentSession{
		ID:          s.ID,
		Kind:        s.Kind,
		Status:      s.Status,
		FromID:      s.FromID,
		ToID:        s.ToID,
		Note:        s.Note,
		Currency:    s.Currency,
		TotalAmount: s.TotalAmount,
		Data:        s.Data,
		DateCreated: s.DateCreated,
		DatePaid:    s.DatePaid,
		DateExpired: s.DateExpired,
	}
}

// mapTransaction converts an sqlc OrderTransaction row to the domain Transaction.
func mapTransaction(tx orderdb.OrderTransaction) ordermodel.Transaction {
	var exchangeRate decimal.Decimal
	if tx.ExchangeRate.Valid {
		if v, err := tx.ExchangeRate.Value(); err == nil && v != nil {
			if d, perr := decimal.NewFromString(fmt.Sprintf("%v", v)); perr == nil {
				exchangeRate = d
			}
		}
	}

	return ordermodel.Transaction{
		ID:            tx.ID,
		SessionID:     tx.SessionID,
		Status:        tx.Status,
		Note:          tx.Note,
		Error:         tx.Error,
		PaymentOption: tx.PaymentOption,
		Data:          tx.Data,
		Amount:        tx.Amount,
		FromCurrency:  tx.FromCurrency,
		ToCurrency:    tx.ToCurrency,
		ExchangeRate:  exchangeRate,
		ReversesID:    tx.ReversesID,
		DateCreated:   tx.DateCreated,
		DateSettled:   tx.DateSettled,
		DateExpired:   tx.DateExpired,
	}
}

func mapOrderItem(it orderdb.OrderItem) ordermodel.OrderItem {
	return ordermodel.OrderItem{
		ID:               it.ID,
		OrderID:          it.OrderID,
		AccountID:        it.AccountID,
		SellerID:         it.SellerID,
		SkuID:            it.SkuID,
		SpuID:            it.SpuID,
		SkuName:          it.SkuName,
		Address:          it.Address,
		Note:             it.Note,
		SerialIDs:        it.SerialIds,
		Quantity:         it.Quantity,
		TransportOption:  it.TransportOption,
		SubtotalAmount:   it.SubtotalAmount,
		TotalAmount:      it.TotalAmount,
		PaymentSessionID: it.PaymentSessionID,
		DateCreated:      it.DateCreated,
		DateCancelled:    it.DateCancelled,
		CancelledByID:    it.CancelledByID,
	}
}

func mapOrder(o orderdb.OrderOrder) ordermodel.Order {
	return ordermodel.Order{
		ID:               o.ID,
		BuyerID:          o.BuyerID,
		SellerID:         o.SellerID,
		TransportID:      o.TransportID,
		Address:          o.Address,
		DateCreated:      o.DateCreated,
		ConfirmedByID:    o.ConfirmedByID,
		ConfirmSessionID: o.ConfirmSessionID,
		Note:             o.Note,
	}
}

func mapRefund(r orderdb.OrderRefund) ordermodel.Refund {
	return ordermodel.Refund{
		ID:            r.ID,
		AccountID:     r.AccountID,
		OrderID:       r.OrderID,
		TransportID:   r.TransportID,
		Method:        r.Method,
		Reason:        r.Reason,
		Address:       r.Address,
		DateCreated:   r.DateCreated,
		Status:        r.Status,
		AcceptedByID:  r.AcceptedByID,
		DateAccepted:  r.DateAccepted,
		RejectionNote: r.RejectionNote,
		ApprovedByID:  r.ApprovedByID,
		DateApproved:  r.DateApproved,
		RefundTxID:    r.RefundTxID,
	}
}

func mapRefundDispute(d orderdb.OrderRefundDispute) ordermodel.RefundDispute {
	return ordermodel.RefundDispute{
		ID:           d.ID,
		AccountID:    d.AccountID,
		RefundID:     d.RefundID,
		Reason:       d.Reason,
		Status:       d.Status,
		Note:         d.Note,
		DateCreated:  d.DateCreated,
		ResolvedByID: d.ResolvedByID,
		DateResolved: d.DateResolved,
	}
}

func toNullUUID(p *uuid.UUID) uuid.NullUUID {
	if p == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *p, Valid: true}
}
