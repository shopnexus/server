package orderbiz

import (
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
)

// enrichItems converts DB items to model items (no separate resources enrichment needed here).
func (b *OrderHandler) enrichItems(dbItems []orderdb.OrderItem) ([]ordermodel.OrderItem, error) {
	if len(dbItems) == 0 {
		return []ordermodel.OrderItem{}, nil
	}

	result := make([]ordermodel.OrderItem, 0, len(dbItems))
	for _, it := range dbItems {
		result = append(result, mapOrderItem(it))
	}

	return result, nil
}
