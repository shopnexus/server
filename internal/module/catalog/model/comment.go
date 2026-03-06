package catalogmodel

import (
	accountmodel "shopnexus-remastered/internal/module/account/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"
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
	DateCreated time.Time              `json:"date_created"`
	DateUpdated time.Time              `json:"date_updated"`
	Resources   []commonmodel.Resource `json:"resources"`
}
