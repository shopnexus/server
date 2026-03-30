package catalogmodel

import (
	accountmodel "shopnexus-server/internal/module/account/model"
	commonmodel "shopnexus-server/internal/module/common/model"
	"time"

	"github.com/google/uuid"
)

type Comment struct {
	ID          uuid.UUID              `json:"id"`
	Profile     accountmodel.Profile   `json:"profile"`
	Body        string                 `json:"body"`
	Upvote      int64                  `json:"upvote"`
	Downvote    int64                  `json:"downvote"`
	Score       float64                `json:"score"`
	OrderID     uuid.UUID               `json:"order_id"`
	DateCreated time.Time              `json:"date_created"`
	DateUpdated time.Time              `json:"date_updated"`
	Resources   []commonmodel.Resource `json:"resources"`
}

type ReviewableOrder struct {
	ID          uuid.UUID `json:"id"`
	Total       int64     `json:"total"`
	DateCreated time.Time `json:"date_created"`
}
