package searchbiz

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/samber/lo"

	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
	"shopnexus-remastered/internal/module/shared/validator"
)

const (
	EmbeddingProductSyncBatchSize = 100
	MetadataProductSyncBatchSize  = 1000
)

func (b *SearchBiz) SetupCron() error {
	go b.StartProductSyncCron(context.Background(), time.Second, true)  // TODO: Make config for duration
	go b.StartProductSyncCron(context.Background(), time.Second, false) // TODO: Make config for duration
	return nil
}

// SyncProductData fetches all product data and sends it to search engine server
func (b *SearchBiz) SyncProductData(ctx context.Context, metadataOnly bool) error {
	if err := b.storage.WithTx(ctx, b.storage, func(txStorage pgsqlc.Storage) error {
		if metadataOnly {
			metadataStales, err := txStorage.ListStaleSearchSync(ctx, db.ListStaleSearchSyncParams{
				RefType:         "Product",
				Limit:           MetadataProductSyncBatchSize,
				IsStaleMetadata: pgtype.Bool{Bool: true, Valid: true},
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
			embeddingStales, err := txStorage.ListStaleSearchSync(ctx, db.ListStaleSearchSyncParams{
				RefType:          "Product",
				Limit:            EmbeddingProductSyncBatchSize,
				IsStaleEmbedding: pgtype.Bool{Bool: true, Valid: true},
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
	Storage      pgsqlc.Storage              `validate:"required"`
	Stales       []db.ListStaleSearchSyncRow `validate:"required"`
	MetadataOnly bool
}

func (b *SearchBiz) UpdateStaleProducts(ctx context.Context, params UpdateStaleProductsParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	log.Printf("🔄 Syncing %d stale products (metadataOnly=%v)...", len(params.Stales), params.MetadataOnly)

	// Fetch product details
	var productDetails []catalogmodel.ProductDetail
	for _, stale := range params.Stales {
		detail, err := b.getProductDetail(ctx, stale.RefID)
		if err != nil {
			log.Printf("❌ Failed to get product detail for product ID %d: %v", stale.RefID, err)
			continue
		}

		productDetails = append(productDetails, detail)
	}

	// map[refID]stale
	staleMap := lo.KeyBy(params.Stales, func(s db.ListStaleSearchSyncRow) int64 { return s.RefID })
	var updateArgs []db.UpdateBatchSystemSearchSyncParams
	for _, detail := range productDetails {
		updateArgs = append(updateArgs, db.UpdateBatchSystemSearchSyncParams{
			ID:               staleMap[detail.ID].ID,
			RefType:          pgtype.Text{String: staleMap[detail.ID].RefType, Valid: true},
			RefID:            pgtype.Int8{Int64: detail.ID, Valid: true},
			IsStaleEmbedding: pgtype.Bool{Bool: params.MetadataOnly, Valid: true},
			IsStaleMetadata:  pgtype.Bool{Bool: false, Valid: true},
		})
	}

	// Update product stale status
	var updateErr error
	params.Storage.UpdateBatchSystemSearchSync(ctx, updateArgs).Exec(func(i int, err error) {
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
func (b *SearchBiz) StartProductSyncCron(ctx context.Context, duration time.Duration, metadataOnly bool) {
	log.Println("🚀 Starting product sync cron job...")

	// Run immediately on startup
	if err := b.SyncProductData(ctx, metadataOnly); err != nil {
		log.Printf("❌ Initial product sync failed: %v", err)
	}

	for {
		// Wait for duration or stop early if context is canceled
		select {
		case <-time.After(duration):
			// continue to next sync
		case <-ctx.Done():
			log.Println("🛑 Stopping product sync cron job...")
			return
		}

		b.syncLock.Lock()
		if err := b.SyncProductData(ctx, metadataOnly); err != nil {
			log.Printf("❌ Product sync failed: %v", err)
		}
		b.syncLock.Unlock()
	}
}
