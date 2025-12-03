package catalogbiz

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"

	"shopnexus-remastered/internal/infras/cachestruct"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commondb "shopnexus-remastered/internal/module/common/db/sqlc"
	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/validator"
)

func (b *CatalogBiz) ProductCardsFromSpuIDs(ctx context.Context, spuIDs []uuid.UUID) (map[uuid.UUID]*catalogmodel.ProductCard, error) {
	var zero map[uuid.UUID]*catalogmodel.ProductCard
	var productMap = make(map[uuid.UUID]*catalogmodel.ProductCard)

	listSpu, err := b.ListProductSpu(ctx, ListProductSpuParams{
		ID: spuIDs,
	})
	if err != nil {
		return zero, err
	}
	spus := listSpu.Data
	spuMap := lo.KeyBy(spus, func(spu catalogmodel.ProductSpu) uuid.UUID { return spu.ID })

	// Get featured SKUs for each spu
	var featuredIDs []uuid.UUID
	for _, spu := range spus {
		if spu.FeaturedSkuID.Valid {
			featuredIDs = append(featuredIDs, spu.FeaturedSkuID.UUID)
		}
	}

	// Get featured SKUs
	featuredSkus, err := b.ListProductSku(ctx, ListProductSkuParams{
		ID: featuredIDs,
	})
	if err != nil {
		return zero, err
	}

	// map[spuID]FeaturedSKU
	featuredMap := lo.KeyBy(featuredSkus, func(row catalogmodel.ProductSku) uuid.UUID { return row.SpuID })

	// Build price request inputs for featured SKUs
	requestPrices := make([]catalogmodel.RequestOrderPrice, 0, len(featuredSkus))
	for _, sku := range featuredSkus {
		requestPrices = append(requestPrices, catalogmodel.RequestOrderPrice{
			SkuID:     sku.ID,
			SpuID:     sku.SpuID,
			UnitPrice: commonmodel.Concurrency(sku.Price),
			Quantity:  1,
			ShipCost:  0,
		})
	}

	priceMap, err := b.promotion.CalculatePromotedPrices(ctx, requestPrices, spuMap)
	if err != nil {
		return zero, err
	}

	// Calculate rating score
	ratings, err := b.storage.Querier().ListRating(ctx, catalogdb.ListRatingParams{
		RefType: catalogdb.CatalogCommentRefTypeProductSpu,
		RefID:   spuIDs,
	})
	if err != nil {
		return zero, err
	}
	ratingMap := lo.KeyBy(ratings, func(r catalogdb.ListRatingRow) uuid.UUID { return r.RefID })

	// Get first image of the product
	resourcesMap, err := b.common.GetResources(ctx, commondb.CommonResourceRefTypeProductSpu, spuIDs)
	if err != nil {
		return zero, err
	}

	// Map promotion codes to ProductCardPromo per SPU
	promoCardsMap := make(map[uuid.UUID][]catalogmodel.ProductCardPromo)
	for _, featured := range featuredSkus {
		price := priceMap[featured.ID]
		if price == nil || len(price.PromotionCodes) == 0 {
			continue
		}

		promoCardsMap[featured.SpuID] = lo.Map(price.PromotionCodes, func(code string, _ int) catalogmodel.ProductCardPromo {
			return catalogmodel.ProductCardPromo{
				Title: code,
			}
		})
	}

	for _, spu := range spus {
		featured := featuredMap[spu.ID]
		rating := ratingMap[spu.ID]
		resources := resourcesMap[spu.ID]

		priceValue := commonmodel.Concurrency(featured.Price)
		originalPrice := commonmodel.Concurrency(featured.Price)
		if priceInfo := priceMap[featured.ID]; priceInfo != nil {
			originalPrice = priceInfo.Request.UnitPrice
			if priceInfo.ProductCost != 0 {
				priceValue = priceInfo.ProductCost
			}
		}

		productMap[spu.ID] = &catalogmodel.ProductCard{
			ID:          spu.ID,
			Slug:        spu.Slug,
			VendorID:    spu.AccountID,
			CategoryID:  spu.Category.ID,
			BrandID:     spu.Brand.ID,
			Name:        spu.Name,
			Description: spu.Description,
			IsActive:    spu.IsActive,
			DateCreated: spu.DateCreated,
			DateUpdated: spu.DateUpdated,

			Promotions:    promoCardsMap[spu.ID],
			Price:         priceValue,
			OriginalPrice: originalPrice,
			Rating: catalogmodel.Rating{
				Score: float32(rating.Score),
				Total: int(rating.Count),
			},
			Resources: resources,
		}
	}

	return productMap, nil
}

type ListProductCardParams struct {
	commonmodel.PaginationParams
	VendorID uuid.NullUUID `validate:"omitnil"`
	Search   null.String   `validate:"omitnil,min=1,max=100"`
}

func (b *CatalogBiz) ListProductCard(ctx context.Context, params ListProductCardParams) (commonmodel.PaginateResult[catalogmodel.ProductCard], error) {
	var zero commonmodel.PaginateResult[catalogmodel.ProductCard]
	var products []catalogmodel.ProductCard
	var err error

	if err = validator.Validate(params); err != nil {
		return zero, err
	}

	var total int64
	var spuIDs []uuid.UUID // To respect order of search result
	var searchArg = catalogdb.SearchCountProductSpuParams{
		Limit:  params.Limit,
		Offset: params.Offset(),
	}

	if params.VendorID.Valid {
		searchArg.AccountID = []uuid.UUID{params.VendorID.UUID}
	}

	// If search is provided, use search service to get product IDs
	if params.Search.Valid {
		searchProducts, err := b.Search(ctx, SearchParams{
			PaginationParams: params.PaginationParams,
			Collection:       "products",
			Query:            params.Search.String,
		})
		if err != nil {
			slog.Error("failed to search products",
				slog.String("query", params.Search.String),
				slog.Any("error", err),
			)
			searchArg.Description = params.Search
			searchArg.Name = params.Search
			searchArg.Slug = params.Search
		} else {
			searchArg.ID = lo.Map(searchProducts, func(p catalogmodel.ProductRecommend, _ int) uuid.UUID { return p.ID })
			spuIDs = lo.Map(searchProducts, func(p catalogmodel.ProductRecommend, _ int) uuid.UUID { return p.ID }) // respect order
		}
		// total = int64(len(searchProducts))
		// TODO: fix the search server to return total instead of calculating here
		total = int64(params.Page.Int32)*int64(params.Limit.Int32) + 1
		if len(searchProducts) < int(params.Limit.Int32) {
			total -= int64(params.Limit.Int32) - int64(len(searchProducts))
		}
	} else {
		total, err = b.storage.Querier().CountProductSpu(ctx, catalogdb.CountProductSpuParams{})
		if err != nil {
			return zero, err
		}
	}

	searchCountSpu, err := b.storage.Querier().SearchCountProductSpu(ctx, searchArg)
	if err != nil {
		return zero, err
	}
	// TODO: handle total from search result

	productCardMap, err := b.ProductCardsFromSpuIDs(ctx, lo.Map(searchCountSpu, func(spu catalogdb.SearchCountProductSpuRow, _ int) uuid.UUID { return spu.CatalogProductSpu.ID }))
	if err != nil {
		return zero, err
	}

	// respect the order from search result, else use the order from DB query
	if len(spuIDs) == 0 {
		spuIDs = lo.Map(searchCountSpu, func(spu catalogdb.SearchCountProductSpuRow, _ int) uuid.UUID { return spu.CatalogProductSpu.ID })
	}

	for _, id := range spuIDs {
		productCard := productCardMap[id]
		if productCard != nil {
			products = append(products, *productCard)
		}
	}

	// List some attributes for compact data
	return commonmodel.PaginateResult[catalogmodel.ProductCard]{
		PageParams: params.PaginationParams,
		Data:       products,
		Total:      null.IntFrom(total),
	}, nil
}

type ListRecommendedProductCardParams struct {
	Account accountmodel.AuthenticatedAccount `validate:"omitempty"`
	Limit   int                               `validate:"omitempty,min=1,max=100"`
}

func (b *CatalogBiz) ListRecommendedProductCard(ctx context.Context, params ListRecommendedProductCardParams) ([]catalogmodel.ProductCard, error) {
	var zero []catalogmodel.ProductCard
	var rcmProducts []catalogmodel.ProductRecommend
	var err error

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// Get current feed offset
	var feedOffset int64 = 0
	if err = b.cache.Get(ctx, fmt.Sprintf(catalogmodel.CacheKeyRecommendOffset, params.Account.ID), &feedOffset); err != nil {
		slog.Error("failed to get feed offset for account",
			slog.String("account_id", params.Account.ID.String()),
			slog.Any("error", err),
		)
	}
	// Retrieve all recommended products from cache
	if err := b.cache.ZRevRangeByScore(ctx, fmt.Sprintf(catalogmodel.CacheKeyRecommendProduct, params.Account.ID), &rcmProducts, cachestruct.ZRangeOptions{
		Offset: null.IntFrom(feedOffset),
		Limit:  null.IntFrom(int64(params.Limit)),
	}); err != nil {
		return zero, err
	}
	feedOffset += int64(len(rcmProducts))

	// if current feed offset is exceeding the size or there is no recommendation in cache, refresh the feed
	if feedOffset >= catalogmodel.CacheRecommendSize || len(rcmProducts) == 0 {
		rcmCtx, cancel := context.WithTimeout(ctx, time.Second*2)
		recommendations, err := b.GetRecommendations(rcmCtx, GetRecommendationsParams{
			Account: params.Account,
			Limit:   catalogmodel.CacheRecommendSize,
		})
		if err != nil {
			slog.Error("failed to get recommendations for account",
				slog.String("account_id", params.Account.ID.String()),
				slog.Any("error", err),
			)
		}
		cancel()

		// Reset feed offset
		feedOffset = 0

		// Remove all old recommendations
		if err = b.cache.Delete(ctx, fmt.Sprintf(catalogmodel.CacheKeyRecommendProduct, params.Account.ID)); err != nil {
			slog.Error("failed to reset feed offset for account", slog.String("account_id", params.Account.ID.String()), slog.Any("error", err))
		}

		// Adding new feed
		for _, p := range recommendations {
			if err = b.cache.ZAdd(ctx, fmt.Sprintf(catalogmodel.CacheKeyRecommendProduct, params.Account.ID), p, float64(p.Score)); err != nil {
				return zero, err
			}
		}
	}

	// Update feed offset in cache
	if err = b.cache.Set(ctx, fmt.Sprintf(catalogmodel.CacheKeyRecommendOffset, params.Account.ID), feedOffset, 0); err != nil {
		slog.Error("failed to update feed offset for account", slog.String("account_id", params.Account.ID.String()), slog.Any("error", err))
	}

	// Amount of most sold products to fill the recommendations
	amount := int32(params.Limit - len(rcmProducts))
	if amount > 0 {
		mostSolds, err := b.inventory.ListMostTaken(ctx, inventorybiz.ListMostTakenParams{
			PaginationParams: commonmodel.PaginationParams{
				Limit: null.Int32From(int32(amount * 100)),
			},
			RefType: inventorydb.InventoryStockRefTypeProductSku,
		})
		if err != nil {
			return zero, err
		}

		skuIDs := lo.Map(mostSolds, func(p inventorydb.InventorySerial, _ int) uuid.UUID { return p.RefID })
		skus, err := b.storage.Querier().ListProductSku(ctx, catalogdb.ListProductSkuParams{
			ID: skuIDs,
		})
		if err != nil {
			return zero, err
		}

		uniqueSpuIDs := lo.UniqMap(skus, func(s catalogdb.CatalogProductSku, _ int) uuid.UUID { return s.SpuID })
		rcmProducts = append(rcmProducts, lo.Map(uniqueSpuIDs, func(spuID uuid.UUID, _ int) catalogmodel.ProductRecommend {
			return catalogmodel.ProductRecommend{
				ID:    spuID,
				Score: 0, // most sold has score 0
			}
		})...)
	}

	productCardMap, err := b.ProductCardsFromSpuIDs(ctx, lo.Map(rcmProducts, func(p catalogmodel.ProductRecommend, _ int) uuid.UUID { return p.ID }))
	if err != nil {
		return zero, err
	}

	products := []catalogmodel.ProductCard{}
	for _, rcm := range rcmProducts {
		if productCardMap[rcm.ID] != nil {
			products = append(products, *productCardMap[rcm.ID])
		}
	}

	return products, nil
}
