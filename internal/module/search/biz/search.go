package searchbiz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"shopnexus-remastered/internal/client/cachestruct"
	"shopnexus-remastered/internal/client/pubsub"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgutil"
)

const InteractionBatchSize = 10

// const SearchServer = "https://b0373f0064cb.ngrok-free.app"
const SearchServer = "http://localhost:8000"

type SearchBiz struct {
	httpClient *http.Client
	storage    *pgutil.Storage
	pubsub     pubsub.Client
	cache      cachestruct.Client

	batchSize int
	mu        sync.Mutex
	buffer    []analyticmodel.Interaction
}

// NewSearchBiz creates a new instance of SearchBiz.
func NewSearchBiz(storage *pgutil.Storage, pubsub pubsub.Client, cache cachestruct.Client) (*SearchBiz, error) {
	b := &SearchBiz{
		httpClient: http.DefaultClient,
		storage:    storage,
		pubsub:     pubsub.Group("search"),
		cache:      cache,
		batchSize:  InteractionBatchSize,
	}

	return b, errutil.Some(
		b.InitPubsub(),
		b.InitCron(),
	)
}

type SearchParams struct {
	sharedmodel.PaginationParams
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
			"dense":  0.7, // TODO: create constants or config
			"sparse": 1,
		},
	}
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return zero, err
	}

	response, err := b.httpClient.Post(SearchServer+"/search", "application/json", bytes.NewReader(jsonBytes))
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
	response, err := b.httpClient.Get(fmt.Sprintf(SearchServer+"/user/%d/recommendations?limit=%d", params.Account.ID, params.Limit))
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

	body, err := json.Marshal(struct {
		Events []analyticmodel.Interaction `json:"events"`
	}{
		Events: events,
	})
	if err != nil {
		return err
	}

	response, err := b.httpClient.Post(SearchServer+"/analytics/process", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to process events, status code: %d", response.StatusCode)
	}

	return nil
}

func (b *SearchBiz) UpdateProducts(ctx context.Context, products []catalogmodel.ProductDetail, metadataOnly bool) error {
	body, err := json.Marshal(struct {
		Products     []catalogmodel.ProductDetail `json:"products"`
		MetadataOnly bool                         `json:"metadata_only"`
	}{
		Products:     products,
		MetadataOnly: metadataOnly,
	})
	if err != nil {
		return err
	}

	response, err := b.httpClient.Post(SearchServer+"/products", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update products, status code: %d", response.StatusCode)
	}

	return nil
}
