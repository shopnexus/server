package catalogbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"
)

type ListProductCardParams struct {
	sharedmodel.PaginationParams
}

func (c *CatalogBiz) ListProductCard(ctx context.Context, params ListProductCardParams) (sharedmodel.PaginateResult[catalogmodel.ProductCard], error) {
	var zero sharedmodel.PaginateResult[catalogmodel.ProductCard]
	var products []catalogmodel.ProductCard

	total, err := c.storage.CountCatalogProductSpu(ctx, db.CountCatalogProductSpuParams{})
	if err != nil {
		return zero, err
	}

	// List all SPUs that user want to see
	spus, err := c.storage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.GetOffset()),
	})
	if err != nil {
		return zero, err
	}
	spuMap := slice.NewSliceMapID(spus, func(spu db.CatalogProductSpu) int64 { return spu.ID })

	// Get flagship products
	flagships, err := c.storage.GetFlagshipProduct(ctx, spuMap.IDs)
	if err != nil {
		return zero, err
	}
	// map[spuID]flagshipProduct
	flagshipMap := slice.NewMap(flagships, func(f db.GetFlagshipProductRow) int64 { return f.SpuID })

	// map[skuID]*ProductPrice
	priceMap, err := c.promotionBiz.CalculatePromotedPrices(ctx, slice.Map(flagships, func(f db.GetFlagshipProductRow) db.CatalogProductSku {
		return db.CatalogProductSku{
			ID:          f.ID,
			SpuID:       f.SpuID,
			Price:       f.Price,
			CanCombine:  f.CanCombine,
			DateCreated: f.DateCreated,
			DateDeleted: f.DateDeleted,
		}
	}), spuMap.Map)
	if err != nil {
		return zero, err
	}

	// Calculate rating score
	ratings, err := c.storage.ListRating(ctx, db.ListRatingParams{
		RefType: db.CatalogCommentRefTypeProductSpu,
		RefID:   spuMap.IDs,
	})
	ratingMap := make(map[int64]catalogmodel.Rating) // map[spuID]Rating
	for _, rating := range ratings {
		ratingMap[rating.RefID] = catalogmodel.Rating{
			Score: float32(rating.Score),
			Total: int(rating.Count),
		}
	}

	// Get first image of the product
	resources, err := c.storage.ListSharedResourceFirst(ctx, db.ListSharedResourceFirstParams{
		OwnerType: db.SharedResourceTypeProductSpu,
		OwnerID:   spuMap.IDs,
	})
	resourceMap := make(map[int64]string) // map[ownerID]url
	for _, res := range resources {
		resourceMap[res.OwnerID] = res.Url
	}

	// Map promotion to ProductCardPromo
	promoCardsMap := make(map[int64][]catalogmodel.ProductCardPromo) // map[spuID]ProductCardPromo
	for _, flagship := range flagships {
		price := priceMap[flagship.SkuID]

		promoCardsMap[flagship.SpuID] = slice.Map(price.Promotions, func(p db.PromotionBase) catalogmodel.ProductCardPromo {
			return catalogmodel.ProductCardPromo{
				ID:          p.ID,
				Title:       p.Title,
				Description: p.Description.String,
			}
		})
	}

	for _, spu := range spus {
		price := priceMap[flagshipMap[spu.ID].SkuID]

		products = append(products, catalogmodel.ProductCard{
			ID:               spu.ID,
			Code:             spu.Code,
			VendorID:         spu.AccountID,
			CategoryID:       spu.CategoryID,
			BrandID:          spu.BrandID,
			Name:             spu.Name,
			Description:      spu.Description,
			IsActive:         spu.IsActive,
			DateManufactured: spu.DateManufactured,
			DateCreated:      spu.DateCreated,
			DateUpdated:      spu.DateUpdated,
			DateDeleted:      spu.DateDeleted,

			Promotions:    promoCardsMap[spu.ID],
			Price:         price.Price,
			OriginalPrice: price.OriginalPrice,
			Rating:        ratingMap[spu.ID],
			Image:         resourceMap[spu.ID],
		})
	}

	// List some attributes for compact data
	return sharedmodel.PaginateResult[catalogmodel.ProductCard]{
		Data:       products,
		Limit:      params.GetLimit(),
		Page:       params.GetPage(),
		Total:      total,
		NextPage:   params.NextPage(total),
		NextCursor: params.NextCursor(total),
	}, nil
}
