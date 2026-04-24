package orderbiz

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
)

// mapTransaction converts an sqlc OrderTransaction row to the domain Transaction.
func mapTransaction(tx orderdb.OrderTransaction) ordermodel.Transaction {
	var fromID *uuid.UUID
	if tx.FromID.Valid {
		v := tx.FromID.UUID
		fromID = &v
	}
	var toID *uuid.UUID
	if tx.ToID.Valid {
		v := tx.ToID.UUID
		toID = &v
	}
	var paymentOption *string
	if tx.PaymentOption.Valid {
		v := tx.PaymentOption.String
		paymentOption = &v
	}
	var instrumentID *uuid.UUID
	if tx.InstrumentID.Valid {
		v := tx.InstrumentID.UUID
		instrumentID = &v
	}
	var datePaid *time.Time
	if tx.DatePaid.Valid {
		v := tx.DatePaid.Time
		datePaid = &v
	}

	// ExchangeRate: pgtype.Numeric; surface as string for precision.
	// Value() returns (driver.Value, error) where Value is a string when Valid.
	var exchangeStr string
	if tx.ExchangeRate.Valid {
		if v, err := tx.ExchangeRate.Value(); err == nil && v != nil {
			exchangeStr = fmt.Sprintf("%v", v)
		}
	}

	return ordermodel.Transaction{
		ID:            tx.ID,
		FromID:        fromID,
		ToID:          toID,
		Type:          tx.Type,
		Status:        tx.Status,
		Note:          tx.Note,
		PaymentOption: paymentOption,
		InstrumentID:  instrumentID,
		Data:          tx.Data,
		Amount:        tx.Amount,
		FromCurrency:  tx.FromCurrency,
		ToCurrency:    tx.ToCurrency,
		ExchangeRate:  exchangeStr,
		DateCreated:   tx.DateCreated,
		DatePaid:      datePaid,
		DateExpired:   tx.DateExpired,
	}
}

func mapOrderItem(it orderdb.OrderItem) ordermodel.OrderItem {
	var orderID *uuid.UUID
	if it.OrderID.Valid {
		v := it.OrderID.UUID
		orderID = &v
	}
	var note *string
	if it.Note.Valid {
		v := it.Note.String
		note = &v
	}
	var dateCancelled *time.Time
	if it.DateCancelled.Valid {
		v := it.DateCancelled.Time
		dateCancelled = &v
	}
	var cancelledByID *uuid.UUID
	if it.CancelledByID.Valid {
		v := it.CancelledByID.UUID
		cancelledByID = &v
	}
	var refundTxID *int64
	if it.RefundTxID.Valid {
		v := it.RefundTxID.Int64
		refundTxID = &v
	}
	return ordermodel.OrderItem{
		ID:              it.ID,
		OrderID:         orderID,
		AccountID:       it.AccountID,
		SellerID:        it.SellerID,
		SkuID:           it.SkuID,
		SkuName:         it.SkuName,
		Address:         it.Address,
		Note:            note,
		SerialIDs:       it.SerialIds,
		Quantity:        it.Quantity,
		TransportOption: it.TransportOption,
		SubtotalAmount:  it.SubtotalAmount,
		PaidAmount:      it.PaidAmount,
		PaymentTxID:     it.PaymentTxID,
		DateCreated:     it.DateCreated,
		DateCancelled:   dateCancelled,
		CancelledByID:   cancelledByID,
		RefundTxID:      refundTxID,
	}
}

func mapOrder(o orderdb.OrderOrder) ordermodel.Order {
	var note *string
	if o.Note.Valid {
		v := o.Note.String
		note = &v
	}
	return ordermodel.Order{
		ID:            o.ID,
		BuyerID:       o.BuyerID,
		SellerID:      o.SellerID,
		TransportID:   o.TransportID,
		Address:       o.Address,
		DateCreated:   o.DateCreated,
		ConfirmedByID: o.ConfirmedByID,
		SellerTxID:    o.SellerTxID,
		Note:          note,
	}
}

func mapRefund(r orderdb.OrderRefund) ordermodel.Refund {
	var address *string
	if r.Address.Valid {
		v := r.Address.String
		address = &v
	}
	var acceptedByID *uuid.UUID
	if r.AcceptedByID.Valid {
		v := r.AcceptedByID.UUID
		acceptedByID = &v
	}
	var dateAccepted *time.Time
	if r.DateAccepted.Valid {
		v := r.DateAccepted.Time
		dateAccepted = &v
	}
	var rejectionNote *string
	if r.RejectionNote.Valid {
		v := r.RejectionNote.String
		rejectionNote = &v
	}
	var approvedByID *uuid.UUID
	if r.ApprovedByID.Valid {
		v := r.ApprovedByID.UUID
		approvedByID = &v
	}
	var dateApproved *time.Time
	if r.DateApproved.Valid {
		v := r.DateApproved.Time
		dateApproved = &v
	}
	var refundTxID *int64
	if r.RefundTxID.Valid {
		v := r.RefundTxID.Int64
		refundTxID = &v
	}
	return ordermodel.Refund{
		ID:            r.ID,
		AccountID:     r.AccountID,
		OrderItemID:   r.OrderItemID,
		TransportID:   r.TransportID,
		Method:        r.Method,
		Reason:        r.Reason,
		Address:       address,
		DateCreated:   r.DateCreated,
		Status:        r.Status,
		AcceptedByID:  acceptedByID,
		DateAccepted:  dateAccepted,
		RejectionNote: rejectionNote,
		ApprovedByID:  approvedByID,
		DateApproved:  dateApproved,
		RefundTxID:    refundTxID,
	}
}

func mapRefundDispute(d orderdb.OrderRefundDispute) ordermodel.RefundDispute {
	var resolvedByID *uuid.UUID
	if d.ResolvedByID.Valid {
		v := d.ResolvedByID.UUID
		resolvedByID = &v
	}
	var dateResolved *time.Time
	if d.DateResolved.Valid {
		v := d.DateResolved.Time
		dateResolved = &v
	}
	return ordermodel.RefundDispute{
		ID:           d.ID,
		AccountID:    d.AccountID,
		RefundID:     d.RefundID,
		Reason:       d.Reason,
		Status:       d.Status,
		Note:         d.Note,
		DateCreated:  d.DateCreated,
		ResolvedByID: resolvedByID,
		DateResolved: dateResolved,
	}
}

func toNullUUID(p *uuid.UUID) uuid.NullUUID {
	if p == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *p, Valid: true}
}
