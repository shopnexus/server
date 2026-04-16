package inventorybiz

import (
	"github.com/google/uuid"
	restate "github.com/restatedev/sdk-go"
	"shopnexus-server/internal/infras/metrics"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
)

type ReleaseInventoryParams struct {
	Items []ReleaseInventoryItem
}

type ReleaseInventoryItem struct {
	RefType inventorydb.InventoryStockRefType
	RefID   uuid.UUID
	Amount  int64
}

func (b *InventoryHandler) ReleaseInventory(ctx restate.Context, params ReleaseInventoryParams) (err error) {
	defer metrics.TrackHandler("inventory", "ReleaseInventory", &err)()

	_, err = restate.Run(ctx, func(ctx restate.RunContext) (any, error) {
		for _, item := range params.Items {
			rows, err := b.storage.Querier().ReleaseInventory(ctx, inventorydb.ReleaseInventoryParams{
				RefID:   item.RefID,
				RefType: item.RefType,
				Amount:  item.Amount,
			})
			if err != nil {
				return nil, sharedmodel.WrapErr("release inventory", err)
			}
			if rows == 0 {
				return nil, sharedmodel.NewError(409, "insufficient reserved inventory to release").Terminal()
			}
		}
		return nil, nil
	})
	return err
}
