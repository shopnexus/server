package searchbiz

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/infras/cachestruct"
	"shopnexus-remastered/internal/infras/pubsub"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/bytedance/sonic"
	"github.com/samber/lo"
)

const InteractionBatchSize = 1

// const b.searchServer = "https://b0373f0064cb.ngrok-free.app"

type SearchBiz struct {
	httpClient *http.Client
	storage    pgsqlc.Storage
	pubsub     pubsub.Client
	cache      cachestruct.Client

	// local configs
	searchServer string
	batchSize    int
	denseWeight  float32
	sparseWeight float32

	mu       sync.Mutex
	buffer   []analyticmodel.Interaction
	syncLock sync.Mutex
}

// NewSearchBiz creates a new instance of SearchBiz.
func NewSearchBiz(
	config *config.Config,
	storage pgsqlc.Storage,
	pubsub pubsub.Client,
	cache cachestruct.Client,
) (*SearchBiz, error) {
	b := &SearchBiz{
		httpClient:   http.DefaultClient,
		storage:      storage,
		pubsub:       pubsub.Group("search"),
		cache:        cache,
		searchServer: config.App.Search.Url,
		denseWeight:  config.App.Search.DenseWeight,
		sparseWeight: config.App.Search.SparseWeight,
		batchSize:    config.App.Search.InteractionBatchSize,
	}

	return b, errors.Join(
		b.InitPubsub(),
		b.SetupCron(),
	)
}

type SearchParams struct {
	commonmodel.PaginationParams
	Collection string
	Query      string
}

func (b *SearchBiz) Search(ctx context.Context, params SearchParams) ([]catalogmodel.ProductRecommend, error) {
	var zero []catalogmodel.ProductRecommend
	body := map[string]interface{}{
		"query":  params.Query,
		"offset": params.Offset(),
		"limit":  params.GetLimit(),
		"weights": map[string]float32{
			"dense":  b.denseWeight,
			"sparse": b.sparseWeight,
		},
	}
	jsonBytes, err := sonic.Marshal(body)
	if err != nil {
		return zero, err
	}

	response, err := b.httpClient.Post(b.searchServer+"/search", "application/json", bytes.NewReader(jsonBytes))
	if err != nil {
		return zero, err
	}
	defer response.Body.Close()

	var results []catalogmodel.ProductRecommend
	if err := json.NewDecoder(response.Body).Decode(&results); err != nil {
		return zero, err
	}

	return results, nil
}

type GetRecommendationsParams struct {
	Account authmodel.AuthenticatedAccount
	Limit   int32
}

func (b *SearchBiz) GetRecommendations(ctx context.Context, params GetRecommendationsParams) ([]catalogmodel.ProductRecommend, error) {
	request, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf(b.searchServer+"/recommend?account_id=%d&limit=%d", params.Account.ID, params.Limit), nil)
	if err != nil {
		return nil, err
	}

	response, err := b.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	var results []catalogmodel.ProductRecommend
	if err := json.NewDecoder(response.Body).Decode(&results); err != nil {
		return nil, err
	}

	return results, nil
}

func (b *SearchBiz) ProcessEvents(ctx context.Context, events []analyticmodel.Interaction) error {
	if len(events) == 0 {
		return nil
	}

	body, err := sonic.Marshal(struct {
		Events []analyticmodel.Interaction `json:"events"`
	}{
		Events: events,
	})
	if err != nil {
		return err
	}

	response, err := b.httpClient.Post(b.searchServer+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to process events, status code: %d", response.StatusCode)
	}

	return nil
}

type UpdateProductsParams struct {
	Products     []catalogmodel.ProductDetail `validate:"required"`
	MetadataOnly bool
}

func (b *SearchBiz) UpdateProducts(ctx context.Context, params UpdateProductsParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	body, err := sonic.Marshal(struct {
		Products     []catalogmodel.ProductDetail `json:"products"`
		MetadataOnly bool                         `json:"metadata_only"`
	}{
		Products:     params.Products,
		MetadataOnly: params.MetadataOnly,
	})
	if err != nil {
		return err
	}

	response, err := b.httpClient.Post(b.searchServer+"/products", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update products, status code: %d", response.StatusCode)
	}

	return nil
}

func (b *SearchBiz) getProductDetail(ctx context.Context, id int64) (catalogmodel.ProductDetail, error) {
	var zero catalogmodel.ProductDetail

	spu, err := b.storage.GetCatalogProductSpu(ctx, db.GetCatalogProductSpuParams{
		ID: pgutil.Int64ToPgInt8(id),
	})
	if err != nil {
		return zero, err
	}

	var skuIDs []int64
	var skusDetail []catalogmodel.ProductDetailSku
	skus, err := b.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		SpuID: []int64{spu.ID},
	})
	if err != nil {
		return zero, err
	}

	for _, sku := range skus {
		skuIDs = append(skuIDs, sku.ID)
	}

	// Get sold count from inventory
	stocks, err := b.storage.ListInventoryStock(ctx, db.ListInventoryStockParams{
		RefType: []db.InventoryStockRefType{db.InventoryStockRefTypeProductSku},
		RefID:   skuIDs,
	})
	if err != nil {
		return zero, err
	}
	stockMap := lo.KeyBy(stocks, func(s db.InventoryStock) int64 { return s.RefID })

	for _, sku := range skus {
		var attributes []catalogmodel.ProductAttribute
		if err := sonic.Unmarshal(sku.Attributes, &attributes); err != nil {
			return zero, err
		}

		skusDetail = append(skusDetail, catalogmodel.ProductDetailSku{
			ID:            sku.ID,
			Price:         sku.Price,
			OriginalPrice: sku.Price,
			Attributes:    attributes,
			Sold:          stockMap[sku.ID].Sold,
		})
	}

	// Get images
	resources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: db.CommonResourceRefTypeProductSpu,
		RefID:   []int64{spu.ID},
	})
	if err != nil {
		return zero, err
	}
	resourceMap := make(map[int64][]commonmodel.Resource) // map[spuID][]Resource
	for _, res := range resources {
		resourceMap[res.RefID] = append(resourceMap[res.RefID], commonmodel.Resource{
			ID:   res.ID.Bytes,
			Mime: res.Mime,
			Size: res.Size,
		})
	}

	// get rating
	rating, err := b.storage.DetailRating(ctx, db.DetailRatingParams{
		RefType: db.CatalogCommentRefTypeProductSpu,
		RefID:   spu.ID,
	})
	ratingBreakdown := make(map[int]int)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	ratingBreakdown[5] = int(rating.FiveCount)
	ratingBreakdown[4] = int(rating.FourCount)
	ratingBreakdown[3] = int(rating.ThreeCount)
	ratingBreakdown[2] = int(rating.TwoCount)
	ratingBreakdown[1] = int(rating.OneCount)

	brand, _ := b.storage.GetCatalogBrand(ctx, db.GetCatalogBrandParams{
		ID: pgutil.Int64ToPgInt8(spu.BrandID),
	})

	category, _ := b.storage.GetCatalogCategory(ctx, db.GetCatalogCategoryParams{
		ID: pgutil.Int64ToPgInt8(spu.CategoryID),
	})

	var specifications []catalogmodel.ProductSpecification
	if err := sonic.Unmarshal(spu.Specifications, &specifications); err != nil {
		return zero, err
	}

	return catalogmodel.ProductDetail{
		ID:          spu.ID,
		Code:        spu.Code,
		VendorID:    spu.AccountID,
		Name:        spu.Name,
		Description: spu.Description,
		Brand:       brand,
		IsActive:    spu.IsActive,
		Category:    category,
		Rating: catalogmodel.ProductRating{
			Score:     rating.Score / 2, // convert 10 scale to 5 scale
			Total:     rating.Count,
			Breakdown: ratingBreakdown,
		},
		Resources:      slice.EnsureSlice(resourceMap[spu.ID]),
		Skus:           skusDetail,
		Specifications: specifications,
	}, nil
}
