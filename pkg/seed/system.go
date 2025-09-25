package seed

import (
	"context"
	"fmt"
	"time"

	"shopnexus-remastered/internal/utils/pgutil"

	"shopnexus-remastered/internal/db"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jaswdr/faker/v2"
)

// SystemSeedData holds seeded system data for other seeders to reference
type SystemSeedData struct {
	SearchSyncs []db.SystemSearchSync
}

// SeedSystemSchema seeds the system schema with fake data
func SeedSystemSchema(ctx context.Context, storage db.Querier, fake *faker.Faker, cfg *SeedConfig, catalogData *CatalogSeedData) (*SystemSeedData, error) {
	fmt.Println("🔧 Seeding system schema...")

	data := &SystemSeedData{
		SearchSyncs: make([]db.SystemSearchSync, 0),
	}

	if catalogData == nil {
		fmt.Println("⚠️ No catalog data found, skipping system seeding")
		return data, nil
	}

	// Prepare bulk search sync data
	var searchSyncParams []db.CreateCopySystemSearchSyncParams

	// Create search sync entries for all products (SPUs) with is_stale = true
	for _, spu := range catalogData.ProductSpus {
		searchSyncParams = append(searchSyncParams, db.CreateCopySystemSearchSyncParams{
			RefType:          "Product",
			RefID:            spu.ID,
			IsStaleEmbedding: true,
			IsStaleMetadata:  true,
			DateCreated:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
	}

	// Create search sync entries for all categories with is_stale = true
	for _, category := range catalogData.Categories {
		searchSyncParams = append(searchSyncParams, db.CreateCopySystemSearchSyncParams{
			RefType:          "Category",
			RefID:            category.ID,
			IsStaleEmbedding: true,
			IsStaleMetadata:  true,
			DateCreated:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
	}

	// Create search sync entries for all brands with is_stale = true
	for _, brand := range catalogData.Brands {
		searchSyncParams = append(searchSyncParams, db.CreateCopySystemSearchSyncParams{
			RefType:          "Brand",
			RefID:            brand.ID,
			IsStaleEmbedding: true,
			IsStaleMetadata:  true,
			DateCreated:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
	}

	// Bulk insert search sync entries
	if len(searchSyncParams) > 0 {
		_, err := storage.CreateCopySystemSearchSync(ctx, searchSyncParams)
		if err != nil {
			return nil, fmt.Errorf("failed to bulk create search sync entries: %w", err)
		}

		// Query back created search sync entries
		searchSyncs, err := storage.ListSystemSearchSync(ctx, db.ListSystemSearchSyncParams{
			Limit:  pgutil.Int32ToPgInt4(int32(len(searchSyncParams) * 2)),
			Offset: pgutil.Int32ToPgInt4(0),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query back created search sync entries: %w", err)
		}

		// Populate data.SearchSyncs with actual database records
		data.SearchSyncs = searchSyncs
	}

	fmt.Printf("✅ System schema seeded: %d search sync entries (Products: %d, Categories: %d, Brands: %d)\n",
		len(data.SearchSyncs), len(catalogData.ProductSpus), len(catalogData.Categories), len(catalogData.Brands))

	return data, nil
}
