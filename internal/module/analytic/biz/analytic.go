package analyticbiz

import (
	"context"
	"log/slog"
	"time"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/pubsub"

	accountmodel "shopnexus-server/internal/module/account/model"
	analyticconfig "shopnexus-server/internal/module/analytic/config"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

type AnalyticStorage = pgsqlc.Storage[*analyticdb.Queries]

type AnalyticBiz struct {
	storage           AnalyticStorage
	pubsub            pubsub.Client
	promotion         *promotionbiz.PromotionBiz
	popularityWeights map[string]float64
}

// NewAnalyticBiz creates a new instance of AnalyticBiz.
func NewAnalyticBiz(
	config *config.Config,
	storage AnalyticStorage,
	pubsub pubsub.Client,
	promotionBiz *promotionbiz.PromotionBiz,
) *AnalyticBiz {
	return &AnalyticBiz{
		storage:           storage,
		pubsub:            pubsub,
		promotion:         promotionBiz,
		popularityWeights: analyticconfig.DefaultPopularityWeights().WeightMap(),
	}
}

type CreateInteraction struct {
	Account   accountmodel.AuthenticatedAccount
	EventType string
	RefType   analyticdb.AnalyticInteractionRefType
	RefID     string
}

type CreateInteractionParams struct {
	Interactions []CreateInteraction
}

// TrackInteraction fires-and-forgets a single analytic interaction.
// It does not block the caller and swallows errors (logged only).
func (b *AnalyticBiz) TrackInteraction(account accountmodel.AuthenticatedAccount, eventType string, refType analyticdb.AnalyticInteractionRefType, refID string) {
	go func() {
		if err := b.CreateInteraction(context.Background(), CreateInteractionParams{
			Interactions: []CreateInteraction{{
				Account:   account,
				EventType: eventType,
				RefType:   refType,
				RefID:     refID,
			}},
		}); err != nil {
			slog.Error("failed to track interaction", "event_type", eventType, "error", err)
		}
	}()
}

// TrackInteractions fires-and-forgets multiple analytic interactions.
func (b *AnalyticBiz) TrackInteractions(interactions []CreateInteraction) {
	go func() {
		if err := b.CreateInteraction(context.Background(), CreateInteractionParams{
			Interactions: interactions,
		}); err != nil {
			slog.Error("failed to track interactions", "error", err)
		}
	}()
}

func (b *AnalyticBiz) CreateInteraction(ctx context.Context, params CreateInteractionParams) error {
	args := lo.Map(params.Interactions, func(interaction CreateInteraction, _ int) analyticdb.CreateBatchInteractionParams {
		return analyticdb.CreateBatchInteractionParams{
			AccountID:     uuid.NullUUID{UUID: interaction.Account.ID, Valid: true},
			AccountNumber: interaction.Account.Number,
			EventType:     interaction.EventType,
			RefType:       interaction.RefType,
			RefID:         interaction.RefID,
			Metadata:      []byte("{}"),
			DateCreated:   time.Now(),
		}
	})

	b.storage.Querier().CreateBatchInteraction(ctx, args).QueryRow(func(_ int, ai analyticdb.AnalyticInteraction, err error) {
		if err == nil {
			if err := b.pubsub.Publish(analyticmodel.TopicAnalyticInteraction, analyticmodel.Interaction{
				ID:            ai.ID,
				AccountID:     ai.AccountID,
				AccountNumber: ai.AccountNumber,
				EventType:     ai.EventType,
				RefType:       ai.RefType,
				RefID:         ai.RefID,
				Metadata:      ai.Metadata,
				DateCreated:   ai.DateCreated,
			}); err != nil {
				slog.Error("failed to publish analytic interaction", "error", err)
			}
		} else {
			slog.Error("failed to create analytic interaction", "error", err)
		}
	})

	return nil
}
