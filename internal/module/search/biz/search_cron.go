package searchbiz

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	"shopnexus-remastered/internal/utils/pgsqlc"
	"shopnexus-remastered/internal/utils/slice"
)

const (
	EmbeddingProductSyncBatchSize = 100
	MetadataProductSyncBatchSize  = 1000
)

func (b *SearchBiz) InitCron() error {
	go b.StartProductSyncCron(context.Background(), time.Second, true)  // TODO: Make config for duration
	go b.StartProductSyncCron(context.Background(), time.Second, false) // TODO: Make config for duration
	return nil
}

// SyncProductData fetches all product data and sends it to search engine server
func (b *SearchBiz) SyncProductData(ctx context.Context, metadataOnly bool) error {
	// ListStaleSyncSearch use SELECT FOR UPDATE SKIP LOCKED, so we need a transaction
	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}
	defer txStorage.Rollback(ctx)

	if metadataOnly {
		metadataStales, err := txStorage.ListStaleSearchSync(ctx, db.ListStaleSearchSyncParams{
			RefType:         "Product",
			Limit:           MetadataProductSyncBatchSize,
			IsStaleMetadata: pgtype.Bool{Bool: true, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("failed to list stale sync search: %w", err)
		}

		if err := b.UpdateStaleProducts(ctx, txStorage, metadataStales, true); err != nil {
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

		if err := b.UpdateStaleProducts(ctx, txStorage, embeddingStales, false); err != nil {
			return fmt.Errorf("failed to update stale products (embedding): %w", err)
		}
	}

	// Till here, all operations are successful, release the locks
	if err := txStorage.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (b *SearchBiz) UpdateStaleProducts(ctx context.Context, txStorage *pgsqlc.Storage, stales []db.ListStaleSearchSyncRow, metadataOnly bool) error {
	if len(stales) == 0 {
		return nil
	}

	log.Printf("🔄 Syncing %d stale products (metadataOnly=%v)...", len(stales), metadataOnly)

	// Fetch product details
	var productDetails []catalogmodel.ProductDetail
	for _, stale := range stales {
		detail, err := b.getProductDetail(ctx, stale.RefID)
		if err != nil {
			log.Printf("❌ Failed to get product detail for product ID %d: %v", stale.RefID, err)
			continue
		}

		productDetails = append(productDetails, detail)
	}

	staleMap := slice.GroupBy(stales, func(s db.ListStaleSearchSyncRow) (int64, db.ListStaleSearchSyncRow) { return s.RefID, s })
	var updateArgs []db.UpdateBatchSystemSearchSyncParams
	for _, detail := range productDetails {
		updateArgs = append(updateArgs, db.UpdateBatchSystemSearchSyncParams{
			ID:               staleMap[detail.ID].ID,
			RefType:          pgtype.Text{String: staleMap[detail.ID].RefType, Valid: true},
			RefID:            pgtype.Int8{Int64: detail.ID, Valid: true},
			IsStaleEmbedding: pgtype.Bool{Bool: metadataOnly, Valid: true},
			IsStaleMetadata:  pgtype.Bool{Bool: false, Valid: true},
		})
	}

	// Update product stale status
	var updateErr error
	txStorage.UpdateBatchSystemSearchSync(ctx, updateArgs).Exec(func(i int, err error) {
		updateErr = err
	})
	if updateErr != nil {
		return fmt.Errorf("failed to update batch system search sync: %w", updateErr)
	}

	// Last step: send to search server (cannot be in the transaction)
	if err := b.UpdateProducts(ctx, productDetails, metadataOnly); err != nil {
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
