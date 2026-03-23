package catalogbiz

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"

	"shopnexus-server/config"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	"shopnexus-server/internal/shared/validator"
)

const (
	EmbeddingProductSyncBatchSize = 32
	MetadataProductSyncBatchSize  = 1000
)

func (b *CatalogBiz) SetupCron() error {
	cfg := config.GetConfig()

	metadataInterval := cfg.App.Search.ProductMetadataSyncInterval
	if metadataInterval <= 0 {
		metadataInterval = time.Second
	}

	embeddingInterval := cfg.App.Search.ProductEmbeddingSyncInterval
	if embeddingInterval <= 0 {
		embeddingInterval = time.Second
	}

	go b.StartProductSyncCron(context.Background(), metadataInterval, true)
	go b.StartProductSyncCron(context.Background(), embeddingInterval, false)
	return nil
}

// SyncProductData fetches all product data and sends it to search engine server
func (b *CatalogBiz) SyncProductData(ctx context.Context, metadataOnly bool) error {
	if metadataOnly {
		metadataStales, err := b.storage.Querier().ListStaleSearchSync(ctx, catalogdb.ListStaleSearchSyncParams{
			RefType:         catalogdb.CatalogSearchSyncRefTypeProductSpu,
			Limit:           MetadataProductSyncBatchSize,
			IsStaleMetadata: null.BoolFrom(true),
		})
		if err != nil {
			return fmt.Errorf("failed to sync product data: %w", err)
		}

		if err := b.UpdateStaleProducts(ctx, UpdateStaleProductsParams{
			Stales:       metadataStales,
			MetadataOnly: true,
		}); err != nil {
			return fmt.Errorf("failed to sync product data: %w", err)
		}
	} else {
		embeddingStales, err := b.storage.Querier().ListStaleSearchSync(ctx, catalogdb.ListStaleSearchSyncParams{
			RefType:          catalogdb.CatalogSearchSyncRefTypeProductSpu,
			Limit:            EmbeddingProductSyncBatchSize,
			IsStaleEmbedding: null.BoolFrom(true),
		})
		if err != nil {
			return fmt.Errorf("failed to sync product data: %w", err)
		}

		if err := b.UpdateStaleProducts(ctx, UpdateStaleProductsParams{
			Stales:       embeddingStales,
			MetadataOnly: false,
		}); err != nil {
			return fmt.Errorf("failed to sync product data: %w", err)
		}
	}

	return nil
}

type UpdateStaleProductsParams struct {
	Stales       []catalogdb.ListStaleSearchSyncRow `validate:"required"`
	MetadataOnly bool
}

func (b *CatalogBiz) UpdateStaleProducts(ctx context.Context, params UpdateStaleProductsParams) error {
	if len(params.Stales) == 0 {
		return nil
	}
	if err := validator.Validate(params); err != nil {
		return err
	}

	log.Printf("🔄 Syncing %d stale products (metadataOnly=%v)...", len(params.Stales), params.MetadataOnly)

	// Fetch product details
	var productDetails []catalogmodel.ProductDetail
	for _, stale := range params.Stales {
		detail, err := b.GetProductDetail(ctx, GetProductDetailParams{
			ID: uuid.NullUUID{UUID: stale.RefID, Valid: true},
		})
		if err != nil {
			slog.Error("Failed to get product detail for product ID", "product_id", stale.RefID, "error", err)
			continue
		}

		productDetails = append(productDetails, detail)
	}

	// map[refID]stale
	staleMap := lo.KeyBy(params.Stales, func(s catalogdb.ListStaleSearchSyncRow) uuid.UUID { return s.RefID })
	var updateArgs []catalogdb.UpdateBatchStaleSearchSyncParams
	for _, detail := range productDetails {
		updateArgs = append(updateArgs, catalogdb.UpdateBatchStaleSearchSyncParams{
			RefType:          staleMap[detail.ID].RefType,
			RefID:            detail.ID,
			IsStaleEmbedding: null.BoolFrom(params.MetadataOnly),
			IsStaleMetadata:  null.BoolFrom(false),
		})
	}

	// Update product stale status
	var updateErr error
	b.storage.Querier().UpdateBatchStaleSearchSync(ctx, updateArgs).Exec(func(i int, err error) {
		updateErr = err
	})
	if updateErr != nil {
		return fmt.Errorf("failed to update batch system search sync: %w", updateErr)
	}

	// Last step: send to search server (cannot be in the transaction)
	if err := b.UpdateProducts(ctx, UpdateProductsParams{
		Products:     productDetails,
		MetadataOnly: params.MetadataOnly,
	}); err != nil {
		return fmt.Errorf("failed to update products: %w", err)
	}

	return nil
}

// StartProductSyncCron starts the cron job for product data sync
func (b *CatalogBiz) StartProductSyncCron(ctx context.Context, duration time.Duration, metadataOnly bool) {
	log.Println("Starting product sync cron job...")

	// Run immediately on startup
	if err := b.SyncProductData(ctx, metadataOnly); err != nil {
		log.Printf("Initial product sync failed: %v", err)
	}

	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	for {
		// Wait for duration or stop early if context is canceled
		select {
		case <-ticker.C:
			// continue to next sync
		case <-ctx.Done():
			log.Println("Stopping product sync cron job...")
			return
		}

		b.syncLock.Lock()
		if err := b.SyncProductData(ctx, metadataOnly); err != nil {
			log.Printf("Product sync failed: %v", err)
		}
		b.syncLock.Unlock()
	}
}
