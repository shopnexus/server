package orderbiz

import (
	"context"
	"encoding/json"
	"fmt"

	"shopnexus-remastered/internal/db"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/pgsqlc"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListInvoiceParams struct {
	commonmodel.PaginationParams
	ID         []int64                  `validate:"dive,min=1"`
	RefType    []db.OrderInvoiceRefType `validate:"dive,validateFn=Valid"`
	RefID      []int64                  `validate:"dive,min=1"`
	Type       []db.OrderInvoiceType    `validate:"dive,validateFn=Valid"`
	ReceiverID []int64                  `validate:"dive,min=1"`
}

func (b *OrderBiz) ListInvoice(ctx context.Context, params ListInvoiceParams) (commonmodel.PaginateResult[db.OrderInvoice], error) {
	var zero commonmodel.PaginateResult[db.OrderInvoice]

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

	return commonmodel.PaginateResult[db.OrderInvoice]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       invoices,
	}, nil
}

type CreateInvoiceParams struct {
	Storage    pgsqlc.Storage
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

	var invoice db.OrderInvoice

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error
		invoice, err = txStorage.CreateDefaultOrderInvoice(ctx, db.CreateDefaultOrderInvoiceParams{
			RefType:    params.RefType,
			RefID:      params.RefID,
			Type:       params.Type,
			ReceiverID: params.ReceiverID,
			Note:       pgutil.NullStringToPgText(params.Note),
			Data:       params.Data,
		})
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create invoice: %w", err)
	}

	return invoice, nil
}
