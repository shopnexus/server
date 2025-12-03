package analyticbiz

import (
	"context"
	"log/slog"
	"time"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/pubsub"

	analyticdb "shopnexus-remastered/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/shared/pgsqlc"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

type AnalyticStorage = pgsqlc.Storage[*analyticdb.Queries]

type AnalyticBiz struct {
	storage   AnalyticStorage
	pubsub    pubsub.Client
	promotion *promotionbiz.PromotionBiz
}

// NewAnalyticBiz creates a new instance of AnalyticBiz.
func NewAnalyticBiz(
	config *config.Config,
	storage AnalyticStorage,
	pubsub pubsub.Client,
	promotionBiz *promotionbiz.PromotionBiz,
) *AnalyticBiz {
	return &AnalyticBiz{
		storage:   storage,
		pubsub:    pubsub,
		promotion: promotionBiz,
	}
}

type CreateInteraction struct {
	AccountID uuid.UUID
	EventType string
	RefType   analyticdb.AnalyticInteractionRefType
	RefID     string
}

type CreateInteractionParams struct {
	Interactions []CreateInteraction
}

func (b *AnalyticBiz) CreateInteraction(ctx context.Context, params CreateInteractionParams) error {
	args := lo.Map(params.Interactions, func(interaction CreateInteraction, _ int) analyticdb.CreateBatchInteractionParams {
		return analyticdb.CreateBatchInteractionParams{
			AccountID:   uuid.NullUUID{UUID: interaction.AccountID, Valid: true},
			EventType:   interaction.EventType,
			RefType:     interaction.RefType,
			RefID:       interaction.RefID,
			Metadata:    []byte("{}"),
			DateCreated: time.Now(),
		}
	})

	b.storage.Querier().CreateBatchInteraction(ctx, args).QueryRow(func(_ int, ai analyticdb.AnalyticInteraction, err error) {
		if err == nil {
			if err := b.pubsub.Publish(analyticmodel.TopicAnalyticInteraction, analyticmodel.Interaction{
				ID:          ai.ID,
				AccountID:   ai.AccountID,
				EventType:   ai.EventType,
				RefType:     ai.RefType,
				RefID:       ai.RefID,
				Metadata:    ai.Metadata,
				DateCreated: ai.DateCreated,
			}); err != nil {
				slog.Error("failed to publish analytic interaction", "error", err)
			}
		} else {
			slog.Error("failed to create analytic interaction", "error", err)
		}
	})

	return nil
}
