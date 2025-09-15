package seed

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"shopnexus-remastered/internal/utils/pgutil"

	"shopnexus-remastered/internal/db"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jaswdr/faker/v2"
)

// SharedSeedData holds seeded shared data for other seeders to reference
type SharedSeedData struct {
	Resources          []db.SharedResource
	ResourceReferences []db.SharedResourceReference
}

// SeedSharedSchema seeds the shared schema with fake data
func SeedSharedSchema(ctx context.Context, storage db.Querier, fake *faker.Faker, cfg *SeedConfig) (*SharedSeedData, error) {
	fmt.Println("🗂️ Seeding shared schema...")

	// Tạo unique tracker (shared resources thường không cần unique constraints đặc biệt)
	// tracker := NewUniqueTracker()

	data := &SharedSeedData{
		Resources:          make([]db.SharedResource, 0),
		ResourceReferences: make([]db.SharedResourceReference, 0),
	}

	mimeTypes := []string{
		"image/jpeg", "image/png", "image/gif", "image/webp",
		"application/pdf", "text/plain", "application/json",
	}

	// Create resources
	resourceCount := cfg.AccountCount + cfg.ProductCount // Resources for avatars and product images
	resourceParams := make([]db.CreateCopySharedResourceParams, resourceCount)

	imagesUrls, err := GetRandomImageURLs2(400, 400, resourceCount)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch random image URLs: %w", err)
	}

	for i := 0; i < resourceCount; i++ {
		mimeType := mimeTypes[fake.RandomDigit()%len(mimeTypes)]

		// Generate unique code for each resource
		code := fmt.Sprintf("resource_%d_%d", i+1, fake.RandomDigit()%10000)

		// Generate file metadata
		fileSize := int64(fake.RandomDigit()%1000000 + 100000) // 100KB - 1MB
		width := int32(400)
		height := int32(400)
		if mimeType == "image/jpeg" || mimeType == "image/png" || mimeType == "image/gif" || mimeType == "image/webp" {
			width = int32(fake.RandomDigit()%800 + 200)  // 200-1000px
			height = int32(fake.RandomDigit()%800 + 200) // 200-1000px
		}

		checksum := fake.Hash().SHA256()
		uploadedBy := int64(fake.RandomDigit()%1000 + 1) // Random uploader ID

		resourceParams[i] = db.CreateCopySharedResourceParams{
			Status:     db.SharedStatusSuccess,
			CreatedAt:  pgtype.Timestamptz{Time: time.Now(), Valid: true},
			Duration:   pgutil.Float64ToPgFloat8(0),
			Code:       code,
			Mime:       mimeType,
			Url:        imagesUrls[i],
			FileSize:   pgutil.Int64ToPgInt8(fileSize),
			Width:      pgutil.Int32ToPgInt4(width),
			Height:     pgutil.Int32ToPgInt4(height),
			Checksum:   pgutil.StringToPgText(checksum),
			UploadedBy: pgutil.Int64ToPgInt8(uploadedBy),
		}
	}

	// Bulk insert resources
	_, err = storage.CreateCopySharedResource(ctx, resourceParams)
	if err != nil {
		return nil, fmt.Errorf("failed to bulk create resources: %w", err)
	}

	// Query back created resources
	resources, err := storage.ListSharedResource(ctx, db.ListSharedResourceParams{
		Limit:  pgutil.Int32ToPgInt4(int32(len(resourceParams) * 2)),
		Offset: pgutil.Int32ToPgInt4(0),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query back created resources: %w", err)
	}

	// Populate data.Resources with actual database records
	data.Resources = resources

	// Create resource references to link resources with other entities
	var resourceRefParams []db.CreateCopySharedResourceReferenceParams

	// Create references for accounts (avatars)
	for i := 0; i < cfg.AccountCount && i < len(data.Resources); i++ {
		resource := data.Resources[i]
		resourceRefParams = append(resourceRefParams, db.CreateCopySharedResourceReferenceParams{
			RsID:      resource.ID,
			RefType:   "Account",
			RefID:     int64(i + 1), // Account ID
			Order:     int32(0),
			IsPrimary: true, // Avatar is primary resource for account
		})
	}

	// Create references for products (product images)
	for i := cfg.AccountCount; i < cfg.AccountCount+cfg.ProductCount && i < len(data.Resources); i++ {
		resource := data.Resources[i]
		productID := int64(i - cfg.AccountCount + 1) // Product ID

		// Create multiple references per product (main image + additional images)
		refCount := fake.RandomDigit()%3 + 1 // 1-3 images per product
		for j := 0; j < refCount; j++ {
			resourceRefParams = append(resourceRefParams, db.CreateCopySharedResourceReferenceParams{
				RsID:      resource.ID,
				RefType:   "ProductSpu",
				RefID:     productID,
				Order:     int32(j),
				IsPrimary: j == 0, // First image is primary
			})
		}
	}

	// Create some additional references for other entity types
	refTypes := db.AllSharedResourceRefTypeValues()
	for i := 0; i < 50 && i < len(data.Resources); i++ { // Create 50 additional references
		resource := data.Resources[i%len(data.Resources)]
		refType := refTypes[fake.RandomDigit()%len(refTypes)]

		resourceRefParams = append(resourceRefParams, db.CreateCopySharedResourceReferenceParams{
			RsID:      resource.ID,
			RefType:   refType,
			RefID:     int64(fake.RandomDigit()%1000 + 1), // Random ref ID
			Order:     int32(fake.RandomDigit() % 10),
			IsPrimary: fake.Boolean().Bool(), // Random primary flag
		})
	}

	// Bulk insert resource references
	if len(resourceRefParams) > 0 {
		_, err = storage.CreateCopySharedResourceReference(ctx, resourceRefParams)
		if err != nil {
			return nil, fmt.Errorf("failed to bulk create resource references: %w", err)
		}

		// Query back created resource references
		resourceRefs, err := storage.ListSharedResourceReference(ctx, db.ListSharedResourceReferenceParams{
			Limit:  pgutil.Int32ToPgInt4(int32(len(resourceRefParams) * 2)),
			Offset: pgutil.Int32ToPgInt4(0),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query back created resource references: %w", err)
		}

		// Populate data.ResourceReferences with actual database records
		data.ResourceReferences = resourceRefs
	}

	fmt.Printf("✅ Shared schema seeded: %d resources, %d resource references\n", len(data.Resources), len(data.ResourceReferences))
	return data, nil
}

// getFileExtension returns file extension based on mime type
func getFileExtension(mimeType string) string {
	extensions := map[string]string{
		"image/jpeg":       "jpg",
		"image/png":        "png",
		"image/gif":        "gif",
		"image/webp":       "webp",
		"application/pdf":  "pdf",
		"text/plain":       "txt",
		"application/json": "json",
	}

	if ext, exists := extensions[mimeType]; exists {
		return ext
	}
	return "bin" // Default binary extension
}

var images []string

func GetRandomImageURLs2(width, height, amount int) ([]string, error) {
	var err error
	if len(images) == 0 {
		images, err = GetRandomImageURLs(width, height, amount)
		if err != nil {
			return nil, err
		}
	}

	// take <amount> random images from images
	selected := make([]string, amount)
	perm := rand.Perm(len(images))
	for i := 0; i < amount; i++ {
		selected[i] = images[perm[i]]
	}

	if len(selected) < amount {
		// If not enough images, repeat some
		for i := len(selected); i < amount; i++ {
			selected = append(selected, images[perm[i%len(images)]])
		}
	}

	return selected, nil
}

func GetRandomImageURLs(width, height, amount int) ([]string, error) {
	urls := make([]string, amount)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Semaphore channel to limit concurrency
	maxConcurrency := 1000
	sem := make(chan struct{}, maxConcurrency)

	for i := 0; i < amount; i++ {
		wg.Add(1)
		sem <- struct{}{} // Acquire a slot

		go func(index int) {
			defer wg.Done()
			defer func() { <-sem }() // Release slot

			url := fmt.Sprintf("https://picsum.photos/%d/%d", width, height)
			resp, err := client.Get(url)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusFound {
				redirectURL := resp.Header.Get("Location")
				mu.Lock()
				urls[index] = redirectURL
				mu.Unlock()
			} else {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
				}
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return urls, nil
}
