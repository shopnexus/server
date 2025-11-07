package analyticbiz

import (
	"context"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"

	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/utils/pgsqlc"
	"shopnexus-remastered/internal/utils/pgutil"
)

type AnalyticBiz struct {
	storage   pgsqlc.Storage
	pubsub    pubsub.Client
	promotion *promotionbiz.PromotionBiz
}

// NewAnalyticBiz creates a new instance of AnalyticBiz.
func NewAnalyticBiz(
	storage pgsqlc.Storage,
	pubsub pubsub.Client,
	promotionBiz *promotionbiz.PromotionBiz,
) *AnalyticBiz {
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
	interaction, err := s.storage.CreateDefaultAnalyticInteraction(ctx, db.CreateDefaultAnalyticInteractionParams{
		AccountID: pgutil.Int64ToPgInt8(params.Account.ID),
		EventType: params.EventType,
		RefType:   params.RefType,
		RefID:     params.RefID,
		Metadata:  []byte("{}"),
	})
	if err != nil {
		return err
	}
	// TODO: add outbox event

	return s.pubsub.Publish(analyticmodel.TopicAnalyticInteraction, analyticmodel.Interaction{
		ID:          interaction.ID,
		AccountID:   pgutil.PgInt8ToNullInt64(interaction.AccountID),
		EventType:   params.EventType,
		RefType:     params.RefType,
		RefID:       params.RefID,
		DateCreated: interaction.DateCreated.Time,
		Metadata:    interaction.Metadata,
	})
}
