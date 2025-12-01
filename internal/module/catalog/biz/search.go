package catalogbiz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	accountmodel "shopnexus-remastered/internal/module/account/model"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/bytedance/sonic"
)

const InteractionBatchSize = 1

// const b.searchServer = "https://b0373f0064cb.ngrok-free.app"

type SearchClient struct {
	// local configs
	searchServer string
	batchSize    int
	denseWeight  float32
	sparseWeight float32

	httpClient *http.Client
	mu         sync.Mutex
	buffer     []analyticmodel.Interaction
	syncLock   sync.Mutex
}

type SearchParams struct {
	commonmodel.PaginationParams
	Collection string
	Query      string
}

func (b *CatalogBiz) Search(ctx context.Context, params SearchParams) ([]catalogmodel.ProductRecommend, error) {
	var zero []catalogmodel.ProductRecommend
	body := map[string]interface{}{
		"query":  params.Query,
		"offset": params.Constrain().Offset(),
		"limit":  params.Constrain().Limit,
		"weights": map[string]float32{
			"dense":  b.searchClient.denseWeight,
			"sparse": b.searchClient.sparseWeight,
		},
	}
	jsonBytes, err := sonic.Marshal(body)
	if err != nil {
		return zero, err
	}

	response, err := b.searchClient.httpClient.Post(b.searchClient.searchServer+"/search", "application/json", bytes.NewReader(jsonBytes))
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
	Account accountmodel.AuthenticatedAccount
	Limit   int32
}

func (b *CatalogBiz) GetRecommendations(ctx context.Context, params GetRecommendationsParams) ([]catalogmodel.ProductRecommend, error) {
	request, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf(b.searchClient.searchServer+"/recommend?account_id=%d&limit=%d", params.Account.ID, params.Limit), nil)
	if err != nil {
		return nil, err
	}

	response, err := b.searchClient.httpClient.Do(request)
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

func (b *CatalogBiz) ProcessEvents(ctx context.Context, events []analyticmodel.Interaction) error {
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

	response, err := b.searchClient.httpClient.Post(b.searchClient.searchServer+"/events", "application/json", bytes.NewReader(body))
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

func (b *CatalogBiz) UpdateProducts(ctx context.Context, params UpdateProductsParams) error {
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

	response, err := b.searchClient.httpClient.Post(b.searchClient.searchServer+"/products", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update products, status code: %d", response.StatusCode)
	}

	return nil
}
