package catalogmodel

import "github.com/google/uuid"

// ProductRecommend represents a recommended product with its score. (should store in redis)
type ProductRecommend struct {
	ID    uuid.UUID `json:"id"` // ProductSpu ID
	Score float32   `json:"score"`
}
