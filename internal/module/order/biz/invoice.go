package orderbiz

import (
	"context"
	"encoding/json"

	"shopnexus-remastered/internal/db"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListInvoiceParams struct {
	sharedmodel.PaginationParams
	ID         []int64                  `validate:"dive,min=1"`
	RefType    []db.OrderInvoiceRefType `validate:"dive,validateFn=Valid"`
	RefID      []int64                  `validate:"dive,min=1"`
	Type       []db.OrderInvoiceType    `validate:"dive,validateFn=Valid"`
	ReceiverID []int64                  `validate:"dive,min=1"`
}

func (b *OrderBiz) ListInvoice(ctx context.Context, params ListInvoiceParams) (sharedmodel.PaginateResult[db.OrderInvoice], error) {
	var zero sharedmodel.PaginateResult[db.OrderInvoice]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountOrderInvoice(ctx, db.CountOrderInvoiceParams{
		ID:         params.ID,
		RefType:    params.RefType,
		RefID:      params.RefID,
		Type:       params.Type,
		ReceiverID: params.ReceiverID,
	})
	if err != nil {
		return zero, err
	}

	invoices, err := b.storage.ListOrderInvoice(ctx, db.ListOrderInvoiceParams{
		Limit:      pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:     pgutil.Int32ToPgInt4(params.Offset()),
		ID:         params.ID,
		RefType:    params.RefType,
		RefID:      params.RefID,
		Type:       params.Type,
		ReceiverID: params.ReceiverID,
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.OrderInvoice]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       invoices,
	}, nil
}

type CreateInvoiceParams struct {
	RefType    db.OrderInvoiceRefType `validate:"required,validateFn=Valid"`
	RefID      int64                  `validate:"required,min=1"`
	Type       db.OrderInvoiceType    `validate:"required,validateFn=Valid"`
	ReceiverID int64                  `validate:"required,min=1"`
	Note       null.String            `validate:"omitnil,max=1000"`
	Data       json.RawMessage        `validate:"required"`
}

func (b *OrderBiz) CreateInvoice(ctx context.Context, params CreateInvoiceParams) (db.OrderInvoice, error) {
	var zero db.OrderInvoice
	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	invoice, err := b.storage.CreateDefaultOrderInvoice(ctx, db.CreateDefaultOrderInvoiceParams{
		RefType:    db.OrderInvoiceRefTypeOrder,
		RefID:      params.RefID,
		Type:       db.OrderInvoiceTypeSale,
		ReceiverID: params.ReceiverID,
		Note:       pgutil.NullStringToPgText(params.Note),
		Data:       params.Data,
	})
	if err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return invoice, nil
}
