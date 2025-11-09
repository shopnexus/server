package searchbiz

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/infras/pubsub"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	"shopnexus-remastered/internal/module/shared/pgutil"
)

func (b *SearchBiz) InitPubsub() error {
	return errors.Join(
		b.pubsub.Subscribe(analyticmodel.TopicAnalyticInteraction, pubsub.DecodeWrap(b.AddInteraction)),
	)
}

type AddInteractionParams = analyticmodel.Interaction

func (b *SearchBiz) AddInteraction(ctx context.Context, params AddInteractionParams) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// buffer the event
	b.buffer = append(b.buffer, params)

	// flushInteractions if reached batch size
	if len(b.buffer) >= b.batchSize {
		toInsert := b.buffer
		b.buffer = make([]analyticmodel.Interaction, 0, b.batchSize) // reset buffer

		// Refresh customer feeds
		if err := b.ProcessEvents(ctx, toInsert); err != nil {
			return err
		}

		// Update

		// async flushInteractions so we don’t block the subscriber
		go b.flushInteractions(ctx, toInsert)
	}
	return nil
}

func (b *SearchBiz) flushInteractions(ctx context.Context, interactions []analyticmodel.Interaction) {
	params := make([]db.CreateCopyDefaultAnalyticInteractionParams, 0, len(interactions))
	for _, i := range interactions {
		metadata, _ := json.Marshal(i.Metadata)

		params = append(params, db.CreateCopyDefaultAnalyticInteractionParams{
			AccountID: pgutil.NullInt64ToPgInt8(i.AccountID),
			SessionID: pgtype.Text{},
			EventType: i.EventType,
			RefType:   i.RefType,
			RefID:     i.RefID,
			Metadata:  metadata,
			UserAgent: pgtype.Text{}, // TODO: missing sessionID + UA + ip
			IpAddress: pgtype.Text{},
		})
	}
	_, err := b.storage.CreateCopyDefaultAnalyticInteraction(ctx, params)
	if err != nil {
		slog.Error("failed to flushInteractions analytic interactions", slog.Any("error", err))
	}
}
