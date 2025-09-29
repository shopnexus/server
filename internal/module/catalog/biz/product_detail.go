package catalogbiz

import (
	"context"
	"database/sql"
	"errors"

	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"
)

type GetProductDetailParams struct {
	ID int64 `validate:"required,gt=0"`
}

func (b *CatalogBiz) GetProductDetail(ctx context.Context, params GetProductDetailParams) (catalogmodel.ProductDetail, error) {
	var zero catalogmodel.ProductDetail

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	spu, err := b.storage.GetCatalogProductSpu(ctx, db.GetCatalogProductSpuParams{
		ID: pgutil.Int64ToPgInt8(params.ID),
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
	stockMap := slice.NewMap(stocks, func(s db.InventoryStock) int64 { return s.RefID })

	// Get attributes for each SKU
	attributes, err := b.storage.ListCatalogProductSkuAttribute(ctx, db.ListCatalogProductSkuAttributeParams{
		SkuID: skuIDs,
	})
	if err != nil {
		return zero, err
	}
	attrMap := make(map[int64]map[string]string)
	for _, attr := range attributes {
		if _, ok := attrMap[attr.SkuID]; !ok {
			attrMap[attr.SkuID] = make(map[string]string)
		}
		attrMap[attr.SkuID][attr.Name] = attr.Value
	}

	for _, sku := range skus {
		skusDetail = append(skusDetail, catalogmodel.ProductDetailSku{
			ID:            sku.ID,
			Price:         sku.Price,
			OriginalPrice: sku.Price,
			Attributes:    attrMap[sku.ID],
			Sold:          stockMap[sku.ID].Sold,
		})
	}

	// Get images
	resources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: db.SharedResourceRefTypeProductSpu,
		RefID:   []int64{spu.ID},
	})
	if err != nil {
		return zero, err
	}
	resourceMap := make(map[int64][]sharedmodel.Resource) // map[spuID][]Resource
	for _, res := range resources {
		resourceMap[res.RefID] = append(resourceMap[res.RefID], sharedmodel.Resource{
			ID:       res.ID,
			Mime:     res.Mime,
			Url:      res.Url,
			FileSize: pgutil.PgInt8ToNullInt64(res.FileSize),
			Width:    pgutil.PgInt4ToNullInt32(res.Width),
			Height:   pgutil.PgInt4ToNullInt32(res.Height),
			Duration: pgutil.PgFloat8ToNullFloat(res.Duration),
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

	category, err := b.storage.GetCatalogCategory(ctx, db.GetCatalogCategoryParams{
		ID: pgutil.Int64ToPgInt8(spu.CategoryID),
	})
	if err != nil {
		return zero, err
	}

	priceMap, err := b.promotionBiz.CalculatePromotedPrices(ctx, skus, map[int64]*db.CatalogProductSpu{
		spu.ID: &spu,
	})
	if err != nil {
		return zero, err
	}
	promoSet := make(map[int64]struct{})
	var promotions []catalogmodel.ProductCardPromo
	for _, price := range priceMap {
		for _, promo := range price.Promotions {
			if _, exists := promoSet[promo.ID]; exists {
				continue
			}
			promoSet[promo.ID] = struct{}{}
			promotions = append(promotions, catalogmodel.ProductCardPromo{
				ID:          promo.ID,
				Title:       promo.Title,
				Description: promo.Description.String,
			})
		}
	}

	brand, err := b.storage.GetCatalogBrand(ctx, db.GetCatalogBrandParams{
		ID: pgutil.Int64ToPgInt8(spu.BrandID),
	})
	if err != nil {
		return zero, err
	}

	return catalogmodel.ProductDetail{
		ID:          spu.ID,
		Name:        spu.Name,
		Description: spu.Description,
		Brand:       brand.Name,
		IsActive:    spu.IsActive,
		Category:    category.Name,
		Rating: catalogmodel.ProductDetailRating{
			Score:     rating.Score / 2, // convert 10 scale to 5 scale
			Total:     rating.Count,
			Breakdown: ratingBreakdown,
		},
		Resources:      slice.NonNil(resourceMap[spu.ID]),
		Promotions:     slice.NonNil(promotions),
		Skus:           skusDetail,
		Specifications: nil,
	}, nil
}
