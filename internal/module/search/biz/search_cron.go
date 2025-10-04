package searchbiz

import (
	"context"
	"fmt"
	"log"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"
)

const (
	EmbeddingProductSyncBatchSize = 100
	MetadataProductSyncBatchSize  = 1000
)

func (b *SearchBiz) InitCron() error {
	go b.StartProductSyncCron(context.Background(), time.Minute, true) // TODO: Make config for duration
	go b.StartProductSyncCron(context.Background(), time.Hour, false)  // TODO: Make config for duration
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

func (b *SearchBiz) UpdateStaleProducts(ctx context.Context, txStorage *pgutil.TxStorage, stales []db.ListStaleSearchSyncRow, metadataOnly bool) error {
	if len(stales) == 0 {
		return nil
	}

	log.Printf("🔄 Syncing %d stale products (metadataOnly=%v)...", len(stales), metadataOnly)

	// Fetch product details
	products, err := b.storage.ListProductDetail(ctx, slice.Map(stales, func(s db.ListStaleSearchSyncRow) int64 { return s.RefID }))
	if err != nil {
		return fmt.Errorf("failed to list product details: %w", err)
	}

	productDetails := make([]catalogmodel.ProductDetail, len(products))
	for i, p := range products {
		productDetails[i] = catalogmodel.ProductDetail{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Brand:       p.BrandName,
			IsActive:    p.IsActive,
			Category:    p.CategoryName,
			Rating: catalogmodel.ProductDetailRating{
				Score: p.RatingScore,
				Total: p.RatingTotal,
			},
			Resources:      make([]sharedmodel.Resource, 0),
			Promotions:     make([]catalogmodel.ProductCardPromo, 0),
			Skus:           make([]catalogmodel.ProductDetailSku, 0),
			Specifications: make(map[string]string),
		}
	}

	if err := b.UpdateProducts(ctx, productDetails, metadataOnly); err != nil {
		return fmt.Errorf("failed to update products: %w", err)
	}

	var updateArgs []db.UpdateBatchSystemSearchSyncParams
	for _, s := range stales {
		updateArgs = append(updateArgs, db.UpdateBatchSystemSearchSyncParams{
			ID:               s.ID,
			RefType:          pgtype.Text{String: s.RefType, Valid: true},
			RefID:            pgtype.Int8{Int64: s.RefID, Valid: true},
			IsStaleEmbedding: pgtype.Bool{Bool: metadataOnly, Valid: true},
			IsStaleMetadata:  pgtype.Bool{Bool: false, Valid: true},
		})
	}

	// No need to check error, as we cannot do anything if update fails
	var updateErr error
	txStorage.UpdateBatchSystemSearchSync(ctx, updateArgs).Exec(func(i int, err error) {
		updateErr = err
	})
	if updateErr != nil {
		log.Printf("❌ Failed to update search sync status: %v", updateErr)
	}

	return nil
}

// StartProductSyncCron starts the cron job for product data sync
func (b *SearchBiz) StartProductSyncCron(ctx context.Context, duration time.Duration, metadataOnly bool) {
	log.Println("🚀 Starting product sync cron job (runs every hour)...")

	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	// Run immediately on startup
	if err := b.SyncProductData(ctx, metadataOnly); err != nil {
		log.Printf("❌ Initial product sync failed: %v", err)
	}

	// Then run every hour
	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 Product sync cron job stopped")
			return
		case <-ticker.C:
			if err := b.SyncProductData(ctx, metadataOnly); err != nil {
				log.Printf("❌ Product sync failed: %v", err)
			}
		}
	}
}
