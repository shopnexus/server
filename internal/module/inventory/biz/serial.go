package inventorybiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListProductSerialParams struct {
	sharedmodel.PaginationParams
	SkuID int64 `validate:"required,gt=0"`
}

func (b *InventoryBiz) ListProductSerial(ctx context.Context, params ListProductSerialParams) (sharedmodel.PaginateResult[inventorymodel.ProductSerial], error) {
	var zero sharedmodel.PaginateResult[inventorymodel.ProductSerial]
	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountInventorySkuSerial(ctx, db.CountInventorySkuSerialParams{
		SkuID: []int64{params.SkuID},
	})
	if err != nil {
		return zero, err
	}

	dbSerials, err := b.storage.ListInventorySkuSerial(ctx, db.ListInventorySkuSerialParams{
		SkuID:  []int64{params.SkuID},
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
	})
	if err != nil {
		return zero, err
	}

	var serials []inventorymodel.ProductSerial
	for _, serial := range dbSerials {
		serials = append(serials, inventorymodel.ProductSerial{
			ID:           serial.ID,
			SerialNumber: serial.SerialNumber,
			SkuID:        serial.SkuID,
			Status:       serial.Status,
			DateCreated:  serial.DateCreated.Time,
		})
	}

	return sharedmodel.PaginateResult[inventorymodel.ProductSerial]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       serials,
	}, nil
}
