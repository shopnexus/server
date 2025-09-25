package analyticbiz

import (
	"context"
	"time"

	"github.com/google/uuid"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"

	authmodel "shopnexus-remastered/internal/module/auth/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/utils/pgutil"
)

type AnalyticBiz struct {
	storage   *pgutil.Storage
	pubsub    pubsub.Client
	promotion *promotionbiz.PromotionBiz
}

// NewAnalyticBiz creates a new instance of AnalyticBiz.
func NewAnalyticBiz(storage *pgutil.Storage, pubsub pubsub.Client, promotionBiz *promotionbiz.PromotionBiz) *AnalyticBiz {
	return &AnalyticBiz{
		storage:   storage,
		pubsub:    pubsub,
		promotion: promotionBiz,
	}
}

type CreateInteractionParams struct {
	Account authmodel.AuthenticatedAccount

	EventType string
	RefType   db.AnalyticInteractionRefType
	RefID     int64
}

func (s *AnalyticBiz) CreateInteraction(ctx context.Context, params CreateInteractionParams) error {
	return s.pubsub.Publish(analyticmodel.TopicAnalyticInteraction, analyticmodel.Interaction{
		ID:          uuid.NewString(),
		AccountID:   params.Account.ID,
		EventType:   params.EventType,
		RefType:     params.RefType,
		RefID:       params.RefID,
		DateCreated: time.Now(),
	})
}
