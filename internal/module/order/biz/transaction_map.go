package orderbiz

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
)

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
		FromID:        tx.FromID,
		ToID:          tx.ToID,
		Type:          tx.Type,
		Status:        tx.Status,
		Note:          tx.Note,
		PaymentOption: tx.PaymentOption,
		WalletID:      tx.WalletID,
		Data:          tx.Data,
		Amount:        tx.Amount,
		FromCurrency:  tx.FromCurrency,
		ToCurrency:    tx.ToCurrency,
		ExchangeRate:  exchangeRate,
		DateCreated:   tx.DateCreated,
		DatePaid:      tx.DatePaid,
		DateExpired:   tx.DateExpired,
	}
}

func mapOrderItem(it orderdb.OrderItem) ordermodel.OrderItem {
	return ordermodel.OrderItem{
		ID:              it.ID,
		OrderID:         it.OrderID,
		AccountID:       it.AccountID,
		SellerID:        it.SellerID,
		SkuID:           it.SkuID,
		SpuID:           it.SpuID,
		SkuName:         it.SkuName,
		Address:         it.Address,
		Note:            it.Note,
		SerialIDs:       it.SerialIds,
		Quantity:        it.Quantity,
		TransportOption: it.TransportOption,
		SubtotalAmount:  it.SubtotalAmount,
		PaidAmount:      it.PaidAmount,
		PaymentTxID:     it.PaymentTxID,
		DateCreated:     it.DateCreated,
		DateCancelled:   it.DateCancelled,
		CancelledByID:   it.CancelledByID,
		RefundTxID:      it.RefundTxID,
	}
}

func mapOrder(o orderdb.OrderOrder) ordermodel.Order {
	return ordermodel.Order{
		ID:            o.ID,
		BuyerID:       o.BuyerID,
		SellerID:      o.SellerID,
		TransportID:   o.TransportID,
		Address:       o.Address,
		DateCreated:   o.DateCreated,
		ConfirmedByID: o.ConfirmedByID,
		SellerTxID:    o.SellerTxID,
		Note:          o.Note,
	}
}

func mapRefund(r orderdb.OrderRefund) ordermodel.Refund {
	return ordermodel.Refund{
		ID:            r.ID,
		AccountID:     r.AccountID,
		OrderItemID:   r.OrderItemID,
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
