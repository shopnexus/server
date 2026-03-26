package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/pg"
	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	promotiondb "shopnexus-server/internal/module/promotion/db/sqlc"

	"github.com/google/uuid"
	"github.com/gosimple/slug"
	null "github.com/guregu/null/v6"
)

// InputProduct represents the structure of products from data.json
type InputProduct struct {
	URL                            string            `json:"url"`
	ID                             int64             `json:"id"`
	Title                          string            `json:"title"`
	Sold                           int64             `json:"sold"`
	Rating                         float64           `json:"rating"`
	Reviews                        int64             `json:"reviews"`
	InitialPrice                   float64           `json:"initial_price"`
	FinalPrice                     float64           `json:"final_price"`
	Currency                       string            `json:"currency"`
	Stock                          any               `json:"stock"`
	Favorite                       int64             `json:"favorite"`
	Image                          []string          `json:"image"`
	Video                          *string           `json:"video"`
	SellerName                     string            `json:"seller_name"`
	ShopURL                        string            `json:"shop_url"`
	Breadcrumb                     []string          `json:"breadcrumb"`
	ProductSpecifications          []Spec            `json:"Product Specifications"`
	ProductDescription             string            `json:"Product Description"`
	SellerRating                   float64           `json:"seller_rating"`
	SellerProducts                 int64             `json:"seller_products"`
	SellerChatsRespondedPercentage float64           `json:"seller_chats_responded_percentage"`
	SellerChatTimeReply            string            `json:"seller_chat_time_reply"`
	SellerJoinedDate               string            `json:"seller_joined_date"`
	SellerFollowers                int64             `json:"seller_followers"`
	Variations                     []Variation       `json:"variations"`
	Domain                         string            `json:"domain"`
	Brand                          string            `json:"brand"`
	CategoryID                     int64             `json:"category_id"`
	FlashSale                      bool              `json:"flash_sale"`
	FlashSaleTime                  string            `json:"flash_sale_time"`
	ProductVariation               []VariationOption `json:"product_variation"`
	GMVCal                         float64           `json:"gmv_cal"`
	CategoryURL                    string            `json:"category_url"`
	Vouchers                       []Voucher         `json:"vouchers"`
	IsAvailable                    string            `json:"is_available"`
	SellerID                       string            `json:"seller_id"`
	ProductRatings                 interface{}       `json:"product_ratings"`
}

type Spec struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Variation struct {
	Name       string   `json:"name"`
	Variations []string `json:"variations"`
}

type VariationOption struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Voucher struct {
	Claimable          bool     `json:"claimable"`
	Currency           string   `json:"currency"`
	Discount           *float64 `json:"discount"`
	DiscountCap        float64  `json:"discount_cap"`
	DiscountPercentage float64  `json:"discount_percentage"`
	MinSpend           float64  `json:"min_spend"`
	ShopVoucher        string   `json:"shop_vouncher"`
	SpecialCondition   *string  `json:"special_condition"`
	TextInfo           string   `json:"text_info"`
	ValidityEnd        string   `json:"validity_end"`
	ValidityStart      string   `json:"validity_start"`
}

func main() {
	ctx := context.Background()
	cfg := config.GetConfig()

	pool, err := pg.New(pg.Options{
		Url:             cfg.Postgres.Url,
		Host:            cfg.Postgres.Host,
		Port:            cfg.Postgres.Port,
		Username:        cfg.Postgres.Username,
		Password:        cfg.Postgres.Password,
		Database:        cfg.Postgres.Database,
		MaxConnections:  cfg.Postgres.MaxConnections,
		MaxConnIdleTime: cfg.Postgres.MaxConnIdleTime,
	})
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	accountStore := accountdb.New(pool)
	catalogStore := catalogdb.New(pool)
	commonStore := commondb.New(pool)
	inventoryStore := inventorydb.New(pool)
	promotionStore := promotiondb.New(pool)

	// Load data.json
	data, err := os.ReadFile("data.json")
	if err != nil {
		log.Fatalf("failed to read data.json: %v", err)
	}

	var products []InputProduct
	if err := json.Unmarshal(data, &products); err != nil {
		log.Fatalf("failed to parse data.json: %v", err)
	}

	log.Printf("Loaded %d products from data.json", len(products))

	// Create vendor account
	accountID := uuid.New()
	account, err := accountStore.CreateAccount(ctx, accountdb.CreateAccountParams{
		ID:          accountID,
		Type:        "Vendor",
		Status:      "Active",
		Email:       null.StringFrom(fmt.Sprintf("vendor+%s@example.com", uuid.New().String())),
		Username:    null.StringFrom(fmt.Sprintf("vendor_%s", uuid.New().String()[:8])),
		Password:    null.StringFrom("password123"),
		DateCreated: time.Now(),
		DateUpdated: time.Now(),
	})
	if err != nil {
		log.Fatalf("failed to create account: %v", err)
	}

	// Create default contact
	contact, err := accountStore.CreateDefaultAccountContact(ctx, accountdb.CreateDefaultAccountContactParams{
		AccountID:   account.ID,
		FullName:    "Vendor User",
		Phone:       "+1234567890",
		Address:     "123 Vendor St, Commerce City",
		AddressType: accountdb.AccountAddressTypeHome,
	})
	if err != nil {
		log.Fatalf("failed to create default contact: %v", err)
	}

	// Create profile for the account
	_, err = accountStore.CreateDefaultProfile(ctx, accountdb.CreateDefaultProfileParams{
		ID:               accountID,
		Gender:           accountdb.NullAccountGender{AccountGender: accountdb.AccountGenderMale, Valid: true},
		Name:             null.StringFrom("Vendor User"),
		DefaultContactID: uuid.NullUUID{UUID: contact.ID, Valid: true},
	})
	if err != nil {
		log.Fatalf("failed to create profile: %v", err)
	}

	// Create vendor profile
	_, err = accountStore.CreateDefaultVendor(ctx, account.ID)
	if err != nil {
		log.Fatalf("failed to create vendor: %v", err)
	}

	log.Printf("Created vendor account with profile: %s", account.ID)

	// Process products
	processedCount := 0
	totalProducts := len(products)

	for i, product := range products {
		if err := processProduct(ctx, product, account.ID, accountStore, catalogStore, commonStore, inventoryStore, promotionStore); err != nil {
			log.Printf("Error processing product %d (%s): %v", i+1, product.Title, err)
		} else {
			processedCount++
		}

		if (i+1)%100 == 0 {
			log.Printf("Progress: %d/%d products processed", i+1, totalProducts)
		}
	}

	log.Printf("✅ Successfully processed %d/%d products", processedCount, totalProducts)
}

func processProduct(
	ctx context.Context,
	input InputProduct,
	accountID uuid.UUID,
	accountStore *accountdb.Queries,
	catalogStore *catalogdb.Queries,
	commonStore *commondb.Queries,
	inventoryStore *inventorydb.Queries,
	promotionStore *promotiondb.Queries,
) error {
	// Upsert brand
	brandID, err := upsertBrand(ctx, catalogStore, input.Brand)
	if err != nil {
		return fmt.Errorf("upsert brand: %w", err)
	}

	// Upsert category
	categoryID, err := upsertCategory(ctx, catalogStore, input.Breadcrumb)
	if err != nil {
		return fmt.Errorf("upsert category: %w", err)
	}

	// Generate slug
	productSlug := slug.Make(input.Title)
	if productSlug == "" {
		productSlug = "product"
	}
	productSlug = fmt.Sprintf("%s.%s", productSlug, uuid.New().String()[:8])

	// Prepare specifications JSON
	specsJSON, err := json.Marshal(input.ProductSpecifications)
	if err != nil {
		specsJSON = []byte("[]")
	}

	// Create ProductSpu
	spuID := uuid.New()
	spu, err := catalogStore.CreateProductSpu(ctx, catalogdb.CreateProductSpuParams{
		ID:             spuID,
		Slug:           productSlug,
		AccountID:      accountID,
		CategoryID:     categoryID,
		BrandID:        brandID,
		FeaturedSkuID:  uuid.NullUUID{Valid: false},
		Name:           input.Title, // Note: Translation would require external service
		Description:    input.ProductDescription,
		IsActive:       strings.ToLower(input.IsAvailable) != "false",
		Specifications: specsJSON,
		DateCreated:    time.Now(),
		DateUpdated:    time.Now(),
		DateDeleted:    null.Time{},
	})
	if err != nil {
		return fmt.Errorf("create product spu: %w", err)
	}

	// Generate all variation combinations and create SKUs
	variationCombos := generateVariationCombinations(input.Variations)
	if len(variationCombos) == 0 {
		// No variations, create single default SKU
		variationCombos = [][]map[string]string{{}}
	}

	// Calculate base price
	basePrice := int64(input.FinalPrice)
	if input.FinalPrice == 0 {
		basePrice = int64(input.InitialPrice)
	}

	// Calculate stock per SKU
	totalStock := pickCurrentStock(input)
	sold := toBigInt(input.Sold)
	stockPerSku := totalStock / int64(len(variationCombos))
	if stockPerSku < 1 {
		stockPerSku = 1
	}
	soldPerSku := sold / int64(len(variationCombos))

	var featuredSkuID uuid.UUID
	for i, combo := range variationCombos {
		attributesJSON, err := json.Marshal(combo)
		if err != nil {
			attributesJSON = []byte("[]")
		}

		// Generate package details
		packageDetails := map[string]interface{}{
			"weight_grams": rand.Intn(1900) + 100, // 100-2000
			"length_cm":    rand.Intn(45) + 5,     // 5-50
			"width_cm":     rand.Intn(45) + 5,     // 5-50
			"height_cm":    rand.Intn(45) + 5,     // 5-50
		}
		packageDetailsJSON, err := json.Marshal(packageDetails)
		if err != nil {
			packageDetailsJSON = []byte("{}")
		}

		// Slight price variation for different SKUs
		price := basePrice + int64(rand.Intn(50)-25)
		if price < 1 {
			price = basePrice
		}

		skuID := uuid.New()
		sku, err := catalogStore.CreateProductSku(ctx, catalogdb.CreateProductSkuParams{
			ID:             skuID,
			SpuID:          spu.ID,
			Price:          price,
			CanCombine:     false,
			Attributes:     attributesJSON,
			PackageDetails: packageDetailsJSON,
			DateCreated:    time.Now(),
			DateDeleted:    null.Time{},
		})
		if err != nil {
			return fmt.Errorf("create product sku: %w", err)
		}

		// First SKU becomes featured
		if i == 0 {
			featuredSkuID = sku.ID
		}

		// Create stock for this SKU
		stock, err := inventoryStore.CreateDefaultStock(ctx, inventorydb.CreateDefaultStockParams{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   sku.ID,
			Stock:   stockPerSku,
		})
		if err != nil {
			return fmt.Errorf("create stock for sku: %w", err)
		}

		// Set taken count for seed data
		if soldPerSku > 0 {
			_, err = inventoryStore.UpdateStock(ctx, inventorydb.UpdateStockParams{
				ID:    stock.ID,
				Taken: null.IntFrom(soldPerSku),
			})
			if err != nil {
				return fmt.Errorf("update stock taken for sku: %w", err)
			}
		}
	}

	// Update featured_sku_id
	_, err = catalogStore.UpdateProductSpu(ctx, catalogdb.UpdateProductSpuParams{
		ID:                spu.ID,
		FeaturedSkuID:     uuid.NullUUID{UUID: featuredSkuID, Valid: true},
		NullFeaturedSkuID: false,
	})
	if err != nil {
		return fmt.Errorf("update featured sku: %w", err)
	}

	// Create resource references for images
	images := input.Image
	if len(images) > 10 {
		images = images[:10]
	}
	for order, imageURL := range images {
		if imageURL == "" {
			continue
		}

		// Get or create resource
		resourceID := uuid.New()
		mimeType := "image/jpeg"
		if strings.Contains(imageURL, ".png") {
			mimeType = "image/png"
		}

		// Try to get existing resource
		resource, err := commonStore.GetResource(ctx, commondb.GetResourceParams{
			ID:        uuid.NullUUID{Valid: false},
			Provider:  null.StringFrom("remote"),
			ObjectKey: null.StringFrom(imageURL),
		})
		if err != nil {
			// Create new resource
			resource, err = commonStore.CreateResource(ctx, commondb.CreateResourceParams{
				ID:         resourceID,
				UploadedBy: uuid.NullUUID{UUID: accountID, Valid: true},
				Provider:   "remote",
				ObjectKey:  imageURL,
				Mime:       mimeType,
				Size:       0,
				Metadata:   []byte("null"),
				Checksum:   null.String{},
				CreatedAt:  time.Now(),
			})
			if err != nil {
				log.Printf("Warning: failed to create resource for %s: %v", imageURL, err)
				continue
			}
		}

		// Create resource reference
		_, err = commonStore.CreateDefaultResourceReference(ctx, commondb.CreateDefaultResourceReferenceParams{
			RsID:    resource.ID,
			RefType: commondb.CommonResourceRefTypeProductSpu,
			RefID:   spu.ID,
			Order:   int32(order),
		})
		if err != nil {
			log.Printf("Warning: failed to create resource reference: %v", err)
		}
	}

	// Create search sync
	_, err = catalogStore.CreateSearchSync(ctx, catalogdb.CreateSearchSyncParams{
		RefType:          "ProductSpu",
		RefID:            spu.ID,
		IsStaleEmbedding: true,
		IsStaleMetadata:  true,
		DateCreated:      time.Now(),
		DateUpdated:      time.Now(),
	})
	if err != nil {
		return fmt.Errorf("create search sync: %w", err)
	}

	// Create tags from breadcrumbs and specifications
	if err := createTags(ctx, catalogStore, spu.ID, input); err != nil {
		log.Printf("Warning: failed to create tags: %v", err)
	}

	// Create promotions from vouchers
	if err := createPromotionsFromVouchers(ctx, input.Vouchers, spu.ID, accountID, promotionStore); err != nil {
		log.Printf("Warning: failed to create promotions: %v", err)
	}

	// Generate comments based on rating/reviews data
	if err := createComments(ctx, catalogStore, accountStore, spu.ID, input); err != nil {
		log.Printf("Warning: failed to create comments: %v", err)
	}

	return nil
}

func upsertBrand(ctx context.Context, store *catalogdb.Queries, brandName string) (uuid.UUID, error) {
	name := brandName
	if name == "" {
		name = "Unknown"
	}
	code := slug.Make(name)

	// Try to get existing brand
	brand, err := store.GetBrand(ctx, catalogdb.GetBrandParams{
		Code: null.StringFrom(code),
	})
	if err == nil {
		// Update existing brand if name changed
		if brand.Name != name {
			brand, err = store.UpdateBrand(ctx, catalogdb.UpdateBrandParams{
				ID:          brand.ID,
				Name:        null.StringFrom(name),
				Description: null.StringFrom(name),
			})
			if err != nil {
				return uuid.Nil, err
			}
		}
		return brand.ID, nil
	}

	// Create new brand
	brand, err = store.CreateDefaultBrand(ctx, catalogdb.CreateDefaultBrandParams{
		Code:        code,
		Name:        name,
		Description: name,
	})
	if err != nil {
		return uuid.Nil, err
	}

	return brand.ID, nil
}

func upsertCategory(ctx context.Context, store *catalogdb.Queries, breadcrumb []string) (uuid.UUID, error) {
	// Use the last breadcrumb element as category
	leaf := "General"
	if len(breadcrumb) > 0 {
		leaf = breadcrumb[len(breadcrumb)-1]
	}

	// Try to get existing category
	category, err := store.GetCategory(ctx, catalogdb.GetCategoryParams{
		Name: null.StringFrom(leaf),
	})
	if err == nil {
		return category.ID, nil
	}

	// Create new category
	category, err = store.CreateDefaultCategory(ctx, catalogdb.CreateDefaultCategoryParams{
		Name:        leaf,
		Description: leaf,
		ParentID:    null.Int{},
	})
	if err != nil {
		return uuid.Nil, err
	}

	return category.ID, nil
}

func mapAttributes(input InputProduct) []map[string]string {
	var attributes []map[string]string

	variations := input.Variations
	chosen := input.ProductVariation

	if len(variations) > 0 && len(chosen) > 0 && chosen[0].Value != "" {
		chosenValues := strings.Split(chosen[0].Value, ",")
		for i, v := range variations {
			attr := map[string]string{
				"name": v.Name,
			}
			if i < len(chosenValues) {
				attr["value"] = strings.TrimSpace(chosenValues[i])
			} else if len(v.Variations) > 0 {
				attr["value"] = v.Variations[0]
			} else {
				attr["value"] = ""
			}

			if attr["name"] == "" {
				attr["name"] = "Option"
			}
			if attr["value"] == "" {
				attr["value"] = "(default)"
			}
			attributes = append(attributes, attr)
		}
	} else if len(variations) > 0 {
		// Fallback: take first option of each variation
		for _, v := range variations {
			attr := map[string]string{
				"name": v.Name,
			}
			if len(v.Variations) > 0 {
				attr["value"] = v.Variations[0]
			} else {
				attr["value"] = ""
			}

			if attr["name"] == "" {
				attr["name"] = "Option"
			}
			if attr["value"] == "" {
				attr["value"] = "(default)"
			}
			attributes = append(attributes, attr)
		}
	}

	return attributes
}

// generateVariationCombinations generates all combinations of variation options
func generateVariationCombinations(variations []Variation) [][]map[string]string {
	if len(variations) == 0 {
		return nil
	}

	// Filter out empty variations
	var validVariations []Variation
	for _, v := range variations {
		if len(v.Variations) > 0 && v.Name != "" {
			validVariations = append(validVariations, v)
		}
	}

	if len(validVariations) == 0 {
		return nil
	}

	// Calculate total combinations
	total := 1
	for _, v := range validVariations {
		total *= len(v.Variations)
	}

	// Limit to 20 combinations to avoid explosion
	if total > 20 {
		// Just take first 2 options from each variation
		for i := range validVariations {
			if len(validVariations[i].Variations) > 2 {
				validVariations[i].Variations = validVariations[i].Variations[:2]
			}
		}
		total = 1
		for _, v := range validVariations {
			total *= len(v.Variations)
		}
	}

	// Generate combinations using cartesian product
	var result [][]map[string]string
	var generate func(depth int, current []map[string]string)
	generate = func(depth int, current []map[string]string) {
		if depth == len(validVariations) {
			combo := make([]map[string]string, len(current))
			copy(combo, current)
			result = append(result, combo)
			return
		}

		v := validVariations[depth]
		for _, opt := range v.Variations {
			attr := map[string]string{
				"name":  v.Name,
				"value": opt,
			}
			generate(depth+1, append(current, attr))
		}
	}

	generate(0, nil)
	return result
}

var stockRegex = regexp.MustCompile(`(?i)existencias|stock`)

func pickCurrentStock(input InputProduct) int64 {
	// Prefer explicit numeric fields
	if input.Stock != nil {
		switch v := input.Stock.(type) {
		case float64:
			return int64(v)
		case string:
			return toBigInt(v)
		default:
			return 0
		}
	}

	// Look into Product Specifications
	for _, spec := range input.ProductSpecifications {
		if stockRegex.MatchString(spec.Name) {
			return toBigInt(spec.Value)
		}
	}

	return 0
}

func toBigInt(value interface{}) int64 {
	if value == nil {
		return 0
	}

	var str string
	switch v := value.(type) {
	case string:
		str = v
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		str = fmt.Sprintf("%v", v)
	}

	// Remove non-numeric characters except minus and decimal point
	re := regexp.MustCompile(`[^0-9.-]`)
	str = re.ReplaceAllString(str, "")

	num, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0
	}

	return int64(num)
}

func createPromotionsFromVouchers(
	ctx context.Context,
	vouchers []Voucher,
	spuID uuid.UUID,
	accountID uuid.UUID,
	store *promotiondb.Queries,
) error {
	if len(vouchers) == 0 {
		return nil
	}

	for _, voucher := range vouchers {
		// Skip if voucher is not valid
		// Try multiple date formats (more flexible like TS new Date())
		var validityEnd, validityStart time.Time
		var err error

		// Try common date formats
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}

		parsed := false
		for _, format := range formats {
			validityEnd, err = time.Parse(format, voucher.ValidityEnd)
			if err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			continue
		}

		parsed = false
		for _, format := range formats {
			validityStart, err = time.Parse(format, voucher.ValidityStart)
			if err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			continue
		}

		// Skip expired vouchers
		if validityEnd.Before(time.Now()) {
			continue
		}

		// Generate promotion code
		promotionCode := fmt.Sprintf("PROMO_%s", uuid.New().String())
		if voucher.TextInfo != "" {
			promotionCode = fmt.Sprintf("VOUCHER_%s_%s", strings.ToUpper(voucher.TextInfo), uuid.New().String()[:8])
		}

		// Create discount data JSON
		minSpend := toBigInt(voucher.MinSpend)
		maxDiscount := toBigInt(voucher.DiscountCap)

		discountData := map[string]interface{}{
			"min_spend":    minSpend,
			"max_discount": maxDiscount,
		}

		if voucher.DiscountPercentage > 0 {
			discountData["discount_percent"] = voucher.DiscountPercentage
		}

		if voucher.Discount != nil && *voucher.Discount > 0 && voucher.DiscountPercentage == 0 {
			discountData["discount_price"] = toBigInt(*voucher.Discount)
		}

		dataJSON, err := json.Marshal(discountData)
		if err != nil {
			return fmt.Errorf("marshal discount data: %w", err)
		}

		// Create promotion with embedded data
		promotion, err := store.CreatePromotion(ctx, promotiondb.CreatePromotionParams{
			ID:          uuid.New(),
			Code:        promotionCode,
			OwnerID:     uuid.NullUUID{UUID: accountID, Valid: true},
			Type:        "Discount",
			Title:       voucher.ShopVoucher,
			Description: null.StringFrom(fmt.Sprintf("Promotion code: %s", voucher.TextInfo)),
			IsActive:    true,
			AutoApply:   !voucher.Claimable,
			Group:       "product_discount",
			Priority:    0,
			Data:        dataJSON,
			DateStarted: validityStart,
			DateEnded:   null.TimeFrom(validityEnd),
			DateCreated: time.Now(),
			DateUpdated: time.Now(),
		})
		if err != nil {
			return fmt.Errorf("create promotion: %w", err)
		}

		// Create promotion ref
		_, err = store.CreateRef(ctx, promotiondb.CreateRefParams{
			PromotionID: promotion.ID,
			RefType:     "ProductSpu",
			RefID:       spuID,
		})
		if err != nil {
			return fmt.Errorf("create promotion ref: %w", err)
		}
	}

	return nil
}

// createTags creates tags from breadcrumbs and specifications
func createTags(
	ctx context.Context,
	store *catalogdb.Queries,
	spuID uuid.UUID,
	input InputProduct,
) error {
	tagSet := make(map[string]bool)

	// Add breadcrumb items as tags
	for _, crumb := range input.Breadcrumb {
		tagID := slug.Make(crumb)
		if tagID != "" && len(tagID) <= 100 {
			tagSet[tagID] = true
		}
	}

	// Add brand as tag
	if input.Brand != "" {
		tagID := slug.Make(input.Brand)
		if tagID != "" && len(tagID) <= 100 {
			tagSet[tagID] = true
		}
	}

	// Add some specification values as tags (style, material, season)
	tagSpecs := []string{"Estilo", "Material", "Temporada", "Style", "Season"}
	for _, spec := range input.ProductSpecifications {
		for _, tagSpec := range tagSpecs {
			if strings.EqualFold(spec.Name, tagSpec) && spec.Value != "" {
				tagID := slug.Make(spec.Value)
				if tagID != "" && len(tagID) <= 100 {
					tagSet[tagID] = true
				}
			}
		}
	}

	// Create tags and link to SPU
	for tagID := range tagSet {
		// Upsert tag
		_, err := store.GetTag(ctx, null.StringFrom(tagID))
		if err != nil {
			_, err = store.CreateTag(ctx, catalogdb.CreateTagParams{
				ID:          tagID,
				Description: null.StringFrom(tagID),
			})
			if err != nil {
				continue // Skip if tag creation fails
			}
		}

		// Link tag to SPU
		_, err = store.CreateProductSpuTag(ctx, catalogdb.CreateProductSpuTagParams{
			SpuID: spuID,
			Tag:   tagID,
		})
		if err != nil {
			// Ignore duplicate errors
			continue
		}
	}

	return nil
}

// createComments generates comments based on rating/reviews data
func createComments(
	ctx context.Context,
	catalogStore *catalogdb.Queries,
	accountStore *accountdb.Queries,
	spuID uuid.UUID,
	input InputProduct,
) error {
	// Skip if no reviews
	if input.Reviews <= 0 {
		return nil
	}

	// Get an account to use as comment author
	accounts, err := accountStore.ListAccount(ctx, accountdb.ListAccountParams{
		Limit: null.Int32From(1),
	})
	if err != nil || len(accounts) == 0 {
		return fmt.Errorf("no accounts available for comments")
	}
	accountID := accounts[0].ID

	// Generate comments based on review count (max 10)
	numComments := int(input.Reviews)
	if numComments > 10 {
		numComments = 10
	}
	if numComments < 1 {
		numComments = 1
	}

	for i := 0; i < numComments; i++ {
		// Generate score based on product rating
		score := input.Rating
		if score == 0 {
			score = float64(rand.Intn(5) + 1)
		}
		// Add some variance
		score += (rand.Float64() - 0.5)
		if score < 1 {
			score = 1
		}
		if score > 5 {
			score = 5
		}

		body := generateReviewBody(score)

		_, err := catalogStore.CreateComment(ctx, catalogdb.CreateCommentParams{
			ID:          uuid.New(),
			AccountID:   accountID,
			RefType:     catalogdb.CatalogCommentRefTypeProductSpu,
			RefID:       spuID,
			Body:        body,
			Upvote:      int64(rand.Intn(50)),
			Downvote:    int64(rand.Intn(5)),
			Score:       score,
			DateCreated: time.Now().Add(-time.Duration(rand.Intn(30*24)) * time.Hour),
			DateUpdated: time.Now(),
		})
		if err != nil {
			continue // Skip failed comments
		}
	}

	return nil
}

// generateReviewBody generates a review body based on score
func generateReviewBody(score float64) string {
	positive := []string{
		"Great product, exactly as described!",
		"Very happy with my purchase. Fast shipping too.",
		"Excellent quality for the price. Would buy again.",
		"Love it! Fits perfectly and looks amazing.",
		"Highly recommend this product to everyone.",
		"Best purchase I've made in a while.",
		"Amazing quality, exceeded my expectations.",
		"Perfect! Just what I was looking for.",
	}

	neutral := []string{
		"Product is okay, nothing special.",
		"Decent quality for the price.",
		"It works as expected.",
		"Average product, meets basic needs.",
		"Not bad, but could be better.",
		"Acceptable quality overall.",
	}

	negative := []string{
		"Not as expected, quality could be better.",
		"Disappointed with the product.",
		"Could be improved in many ways.",
		"Not worth the price in my opinion.",
		"Had some issues with the product.",
	}

	var pool []string
	if score >= 4 {
		pool = positive
	} else if score >= 3 {
		pool = neutral
	} else {
		pool = negative
	}

	return pool[rand.Intn(len(pool))]
}
