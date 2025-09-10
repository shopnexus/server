package seed

import (
	"context"
	"fmt"
	"net/http"
	"shopnexus-remastered/internal/utils/pgutil"
	"sync"

	"shopnexus-remastered/internal/db"

	"github.com/jaswdr/faker/v2"
)

// SharedSeedData holds seeded shared data for other seeders to reference
type SharedSeedData struct {
	Resources []db.SharedResource
}

// SeedSharedSchema seeds the shared schema with fake data
func SeedSharedSchema(ctx context.Context, storage db.Querier, fake *faker.Faker, cfg *SeedConfig) (*SharedSeedData, error) {
	fmt.Println("🗂️ Seeding shared schema...")

	// Tạo unique tracker (shared resources thường không cần unique constraints đặc biệt)
	// tracker := NewUniqueTracker()

	data := &SharedSeedData{
		Resources: make([]db.SharedResource, 0),
	}

	resourceTypes := db.AllSharedResourceTypeValues()
	mimeTypes := []string{
		"image/jpeg", "image/png", "image/gif", "image/webp",
		"application/pdf", "text/plain", "application/json",
	}

	// CreateAccount resources
	resourceCount := cfg.AccountCount + cfg.ProductCount // Resources for avatars and product images
	resourceParams := make([]db.CreateCopySharedResourceParams, resourceCount)

	imagesUrls, err := GetRandomImageURLs2(400, 400, resourceCount)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch random image URLs: %w", err)
	}

	for i := 0; i < resourceCount; i++ {
		resourceType := resourceTypes[fake.RandomDigit()%len(resourceTypes)]
		mimeType := mimeTypes[fake.RandomDigit()%len(mimeTypes)]

		// Ensure image mime types for image-related resources
		//imageMimeTypes := []string{"image/jpeg", "image/png", "image/gif", "image/webp"}
		//mimeType = imageMimeTypes[fake.RandomDigit()%len(imageMimeTypes)]

		ownerID := int64(fake.RandomDigit()%1000 + 1) // Random owner ID
		order := fake.RandomDigit() % 10              // Order for multiple resources of same owner

		resourceParams[i] = db.CreateCopySharedResourceParams{
			MimeType:  mimeType,
			OwnerID:   ownerID,
			OwnerType: resourceType,
			Url:       imagesUrls[i],
			Order:     int32(order),
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

	fmt.Printf("✅ Shared schema seeded: %d resources\n", len(data.Resources))
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

func GetRandomImageURLs2(width, height, amount int) ([]string, error) {
	urls := make([]string, amount)
	for i := 0; i < amount; i++ {
		urls[i] = fmt.Sprintf("https://picsum.photos/%d/%d", width, height)
	}
	return urls, nil
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
	maxConcurrency := 20
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
