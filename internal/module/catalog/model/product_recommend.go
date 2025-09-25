package catalogmodel

// ProductRecommend represents a recommended product with its score. (should store in redis)
type ProductRecommend struct {
	ID    int64   `json:"id"` // ProductSpu ID
	Score float32 `json:"score"`
}
