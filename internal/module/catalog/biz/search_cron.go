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

	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	"shopnexus-remastered/internal/shared/validator"
)

const (
	EmbeddingProductSyncBatchSize = 100
	MetadataProductSyncBatchSize  = 1000
)

func (b *CatalogBiz) SetupCron() error {
	go b.StartProductSyncCron(context.Background(), time.Second, true)  // TODO: Make config for duration
	go b.StartProductSyncCron(context.Background(), time.Second, false) // TODO: Make config for duration
	return nil
}

// SyncProductData fetches all product data and sends it to search engine server
func (b *CatalogBiz) SyncProductData(ctx context.Context, metadataOnly bool) error {
	if err := b.storage.WithTx(ctx, b.storage, func(txStorage CatalogStorage) error {
		if metadataOnly {
			metadataStales, err := txStorage.Querier().ListStaleSearchSync(ctx, catalogdb.ListStaleSearchSyncParams{
				RefType:         "Product",
				Limit:           MetadataProductSyncBatchSize,
				IsStaleMetadata: null.BoolFrom(true),
			})
			if err != nil {
				return fmt.Errorf("failed to list stale sync search: %w", err)
			}

			if err := b.UpdateStaleProducts(ctx, UpdateStaleProductsParams{
				Storage:      txStorage,
				Stales:       metadataStales,
				MetadataOnly: true,
			}); err != nil {
				return fmt.Errorf("failed to update stale products (metadata): %w", err)
			}
		} else {
			embeddingStales, err := txStorage.Querier().ListStaleSearchSync(ctx, catalogdb.ListStaleSearchSyncParams{
				RefType:          "Product",
				Limit:            EmbeddingProductSyncBatchSize,
				IsStaleEmbedding: null.BoolFrom(true),
			})
			if err != nil {
				return fmt.Errorf("failed to list stale sync search: %w", err)
			}

			if err := b.UpdateStaleProducts(ctx, UpdateStaleProductsParams{
				Storage:      txStorage,
				Stales:       embeddingStales,
				MetadataOnly: false,
			}); err != nil {
				return fmt.Errorf("failed to update stale products (embedding): %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to sync product data: %w", err)
	}

	return nil
}

type UpdateStaleProductsParams struct {
	Storage      CatalogStorage
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
		detail, err := b.GetProductDetail(ctx, stale.RefID)
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
	params.Storage.Querier().UpdateBatchStaleSearchSync(ctx, updateArgs).Exec(func(i int, err error) {
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

	for {
		// Wait for duration or stop early if context is canceled
		select {
		case <-time.After(duration):
			// continue to next sync
		case <-ctx.Done():
			log.Println("Stopping product sync cron job...")
			return
		}

		b.searchClient.syncLock.Lock()
		if err := b.SyncProductData(ctx, metadataOnly); err != nil {
			log.Printf("Product sync failed: %v", err)
		}
		b.searchClient.syncLock.Unlock()
	}
}
