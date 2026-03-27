package catalogbiz

import (
	"context"
	"log"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"

	"shopnexus-server/config"
	restateclient "shopnexus-server/internal/infras/restate"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

const (
	EmbeddingProductSyncBatchSize = 32
	MetadataProductSyncBatchSize  = 1000
)

// SetupCron starts background cron jobs for syncing product metadata and embeddings.
func (b *CatalogHandler) SetupCron() error {
	cfg := config.GetConfig()

	metadataInterval := cfg.App.Search.ProductMetadataSyncInterval
	if metadataInterval <= 0 {
		metadataInterval = time.Second
	}

	embeddingInterval := cfg.App.Search.ProductEmbeddingSyncInterval
	if embeddingInterval <= 0 {
		embeddingInterval = time.Second
	}

	go b.startProductSyncCron(context.Background(), metadataInterval, true)
	go b.startProductSyncCron(context.Background(), embeddingInterval, false)
	return nil
}

// syncProductData fetches stale products and syncs them via Restate ingress
func (b *CatalogHandler) syncProductData(ctx context.Context, metadataOnly bool) error {
	if metadataOnly {
		metadataStales, err := b.storage.Querier().ListStaleSearchSync(ctx, catalogdb.ListStaleSearchSyncParams{
			RefType:         catalogdb.CatalogSearchSyncRefTypeProductSpu,
			Limit:           MetadataProductSyncBatchSize,
			IsStaleMetadata: null.BoolFrom(true),
		})
		if err != nil {
			return sharedmodel.WrapErr("sync product data", err)
		}

		if err := b.updateStaleProducts(ctx, UpdateStaleProductsParams{
			Stales:       metadataStales,
			MetadataOnly: true,
		}); err != nil {
			return sharedmodel.WrapErr("sync product data", err)
		}
	} else {
		embeddingStales, err := b.storage.Querier().ListStaleSearchSync(ctx, catalogdb.ListStaleSearchSyncParams{
			RefType:          catalogdb.CatalogSearchSyncRefTypeProductSpu,
			Limit:            EmbeddingProductSyncBatchSize,
			IsStaleEmbedding: null.BoolFrom(true),
		})
		if err != nil {
			return sharedmodel.WrapErr("sync product data", err)
		}

		if err := b.updateStaleProducts(ctx, UpdateStaleProductsParams{
			Stales:       embeddingStales,
			MetadataOnly: false,
		}); err != nil {
			return sharedmodel.WrapErr("sync product data", err)
		}
	}

	return nil
}

type UpdateStaleProductsParams struct {
	Stales       []catalogdb.ListStaleSearchSyncRow `validate:"required"`
	MetadataOnly bool
}

// updateStaleProducts fetches product details via Restate and syncs to search engine
func (b *CatalogHandler) updateStaleProducts(ctx context.Context, params UpdateStaleProductsParams) error {
	if len(params.Stales) == 0 {
		return nil
	}
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate update stale products", err)
	}

	log.Printf("🔄 Syncing %d stale products (metadataOnly=%v)...", len(params.Stales), params.MetadataOnly)

	// Fetch product details via Restate ingress
	var productDetails []catalogmodel.ProductDetail
	for _, stale := range params.Stales {
		detail, err := restateclient.Call[catalogmodel.ProductDetail](ctx, b.restateClient, "Catalog", "GetProductDetail", GetProductDetailParams{
			ID: uuid.NullUUID{UUID: stale.RefID, Valid: true},
		})
		if err != nil {
			slog.Error("get product detail for sync", "product_id", stale.RefID, "error", err)
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
		return sharedmodel.WrapErr("update batch system search sync", updateErr)
	}

	// Last step: send to search server via Restate ingress
	if err := restateclient.Send(ctx, b.restateClient, "Catalog", "UpdateProducts", UpdateProductsParams{
		Products:     productDetails,
		MetadataOnly: params.MetadataOnly,
	}); err != nil {
		return sharedmodel.WrapErr("update products", err)
	}

	return nil
}

// startProductSyncCron starts the cron job for product data sync
func (b *CatalogHandler) startProductSyncCron(ctx context.Context, duration time.Duration, metadataOnly bool) {
	log.Println("Starting product sync cron job...")

	// Run immediately on startup
	if err := b.syncProductData(ctx, metadataOnly); err != nil {
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
		if err := b.syncProductData(ctx, metadataOnly); err != nil {
			log.Printf("Product sync failed: %v", err)
		}
		b.syncLock.Unlock()
	}
}
