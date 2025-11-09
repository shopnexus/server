package catalogbiz

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/guregu/null/v6"
	"github.com/samber/lo"

	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/infras/cachestruct"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
	searchbiz "shopnexus-remastered/internal/module/search/biz"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"
)

func (b *CatalogBiz) ProductCardsFromSpuIDs(ctx context.Context, spuIDs []int64) (map[int64]*catalogmodel.ProductCard, error) {
	var zero map[int64]*catalogmodel.ProductCard
	var productMap = make(map[int64]*catalogmodel.ProductCard)

	spus, err := b.storage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
		ID: spuIDs,
	})
	if err != nil {
		return zero, err
	}
	spuMap := lo.KeyBy(spus, func(spu db.CatalogProductSpu) int64 { return spu.ID })

	// Get featured SKUs for each spu
	var featuredIDs []int64
	for _, spu := range spus {
		if spu.FeaturedSkuID.Valid {
			featuredIDs = append(featuredIDs, spu.FeaturedSkuID.Int64)
		}
	}

	// Get featured SKUs
	featuredSkus, err := b.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		ID: featuredIDs,
	})
	if err != nil {
		return zero, err
	}
	// map[spuID]FeaturedSKU
	featuredMap := lo.KeyBy(featuredSkus, func(f db.CatalogProductSku) int64 { return f.SpuID })

	// map[skuID]*ProductPrice
	priceMap, err := b.promotion.CalculatePromotedPrices(ctx, lo.Map(featuredSkus, func(f db.CatalogProductSku, _ int) db.CatalogProductSku {
		return db.CatalogProductSku{
			ID:          f.ID,
			SpuID:       f.SpuID,
			Price:       f.Price,
			CanCombine:  f.CanCombine,
			DateCreated: f.DateCreated,
			DateDeleted: f.DateDeleted,
			Attributes:  f.Attributes,
		}
	}), spuMap)
	if err != nil {
		return zero, err
	}

	// Calculate rating score
	ratings, err := b.storage.ListRating(ctx, db.ListRatingParams{
		RefType: db.CatalogCommentRefTypeProductSpu,
		RefID:   spuIDs,
	})
	if err != nil {
		return zero, err
	}
	ratingMap := lo.KeyBy(ratings, func(r db.ListRatingRow) int64 { return r.RefID })

	// Get first image of the product
	resources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: db.CommonResourceRefTypeProductSpu,
		RefID:   spuIDs,
	})
	if err != nil {
		return zero, err
	}
	resourcesMap := lo.GroupBy(resources, func(r db.ListSortedResourcesRow) int64 { return r.RefID })

	// Map promotion to ProductCardPromo
	promoCardsMap := make(map[int64][]catalogmodel.ProductCardPromo) // map[spuID]ProductCardPromo
	for _, featured := range featuredSkus {
		price := priceMap[featured.ID]

		promoCardsMap[featured.SpuID] = lo.Map(price.Promotions, func(p promotionmodel.PromotionBase, _ int) catalogmodel.ProductCardPromo {
			return catalogmodel.ProductCardPromo{
				ID:          p.ID,
				Title:       p.Title,
				Description: p.Description.String,
			}
		})
	}

	for _, spu := range spus {
		featured := featuredMap[spu.ID]
		rating := ratingMap[spu.ID]
		resources := resourcesMap[spu.ID]

		var price catalogmodel.ProductPrice
		if priceMap[featured.ID] != nil {
			price = *priceMap[featured.ID]
		}

		productMap[spu.ID] = &catalogmodel.ProductCard{
			ID:          spu.ID,
			Code:        spu.Code,
			VendorID:    spu.AccountID,
			CategoryID:  spu.CategoryID,
			BrandID:     spu.BrandID,
			Name:        spu.Name,
			Description: spu.Description,
			IsActive:    spu.IsActive,
			DateCreated: spu.DateCreated.Time,
			DateUpdated: spu.DateUpdated.Time,
			DateDeleted: spu.DateDeleted.Time,

			Promotions:    promoCardsMap[spu.ID],
			Price:         price.Price,
			OriginalPrice: price.OriginalPrice,
			Rating: catalogmodel.Rating{
				Score: float32(rating.Score),
				Total: int(rating.Count),
			},
			Resources: lo.Map(resources, func(r db.ListSortedResourcesRow, _ int) commonmodel.Resource {
				return commonmodel.Resource{
					ID:       r.ID.Bytes,
					Url:      b.common.MustGetFileURL(ctx, r.Provider, r.ObjectKey),
					Mime:     r.Mime,
					Size:     r.Size,
					Checksum: pgutil.PgTextToNullString(r.Checksum),
				}
			}),
		}
	}

	return productMap, nil
}

type ListProductCardParams struct {
	commonmodel.PaginationParams
	VendorID null.Int64  `validate:"omitnil,min=1"`
	Search   null.String `validate:"omitnil,min=1,max=100"`
}

func (b *CatalogBiz) ListProductCard(ctx context.Context, params ListProductCardParams) (commonmodel.PaginateResult[catalogmodel.ProductCard], error) {
	var zero commonmodel.PaginateResult[catalogmodel.ProductCard]
	var products []catalogmodel.ProductCard
	var err error

	if err = validator.Validate(params); err != nil {
		return zero, err
	}

	var spus []db.CatalogProductSpu
	var total int64
	var spuIDs []int64 // To respect order of search result
	var searchArg = db.SearchCatalogProductSpuParams{
		Limit:     pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:    pgutil.Int32ToPgInt4(params.Offset()),
		AccountID: pgutil.NullInt64ToSlice(params.VendorID),
	}

	// If search is provided, use search service to get product IDs
	if params.Search.Valid {
		searchProducts, err := b.search.Search(ctx, searchbiz.SearchParams{
			PaginationParams: params.PaginationParams,
			Collection:       "products",
			Query:            params.Search.String,
		})
		if err != nil {
			slog.Error("failed to search products",
				slog.String("query", params.Search.String),
				slog.Any("error", err),
			)
			searchArg.Description = pgutil.NullStringToPgText(params.Search)
		} else {
			searchArg.ID = lo.Map(searchProducts, func(p catalogmodel.ProductRecommend, _ int) int64 { return p.ID })
			spuIDs = lo.Map(searchProducts, func(p catalogmodel.ProductRecommend, _ int) int64 { return p.ID }) // respect order
		}
	}

	total, err = b.storage.CountCatalogProductSpu(ctx, db.CountCatalogProductSpuParams{})
	if err != nil {
		return zero, err
	}

	spus, err = b.storage.SearchCatalogProductSpu(ctx, searchArg)
	if err != nil {
		return zero, err
	}

	productCardMap, err := b.ProductCardsFromSpuIDs(ctx, lo.Map(spus, func(spu db.CatalogProductSpu, _ int) int64 { return spu.ID }))
	if err != nil {
		return zero, err
	}

	// respect the order from search result, else use the order from DB query
	if len(spuIDs) == 0 {
		spuIDs = lo.Map(spus, func(spu db.CatalogProductSpu, _ int) int64 { return spu.ID })
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
	Account authmodel.AuthenticatedAccount
	Limit   int `validate:"omitempty,min=1,max=100"`
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
			slog.Int64("account_id", params.Account.ID),
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
		recommendations, err := b.search.GetRecommendations(rcmCtx, searchbiz.GetRecommendationsParams{
			Account: params.Account,
			Limit:   catalogmodel.CacheRecommendSize,
		})
		if err != nil {
			slog.Error("failed to get recommendations for account",
				slog.Int64("account_id", params.Account.ID),
				slog.Any("error", err),
			)
		}
		cancel()

		// Reset feed offset
		feedOffset = 0

		// Remove all old recommendations
		if err = b.cache.Delete(ctx, fmt.Sprintf(catalogmodel.CacheKeyRecommendProduct, params.Account.ID)); err != nil {
			slog.Error("failed to reset feed offset for account", slog.Int64("account_id", params.Account.ID), slog.Any("error", err))
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
		slog.Error("failed to update feed offset for account", slog.Int64("account_id", params.Account.ID), slog.Any("error", err))
	}

	// Amount of most sold products to fill the recommendations
	amount := int32(params.Limit - len(rcmProducts))
	if amount > 0 {
		mostSolds, err := b.storage.ListMostSoldProducts(ctx, db.ListMostSoldProductsParams{
			Limit: amount,
			TopN:  amount * 10, // get more to avoid dup with rcmProducts
		})
		if err != nil {
			return zero, err
		}

		for _, p := range mostSolds {
			rcmProducts = append(rcmProducts, catalogmodel.ProductRecommend{
				ID:    p.SpuID,
				Score: 0, // most sold has score 0
			})
		}
	}

	productCardMap, err := b.ProductCardsFromSpuIDs(ctx, lo.Map(rcmProducts, func(p catalogmodel.ProductRecommend, _ int) int64 { return p.ID }))
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
