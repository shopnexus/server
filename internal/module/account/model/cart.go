package accountmodel

type CartItem struct {
	SkuID         int64   `json:"sku_id"`
	Name          string  `json:"name"`
	SkuName       string  `json:"sku_name"`
	OriginalPrice int64   `json:"original_price"`
	Price         int64   `json:"price"`
	Quantity      int64   `json:"quantity"`
	Image         string  `json:"image"`
	Category      string  `json:"category"`
	Promotions    []int64 `json:"promotions"`
	BulkPrice     *int64  `json:"bulk_price"`
	BulkThreshold *int64  `json:"bulk_threshold"`
}
