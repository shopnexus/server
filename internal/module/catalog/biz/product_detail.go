package catalogbiz

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/samber/lo"
)

func (b *CatalogBiz) GetProductDetail(ctx context.Context, id int64) (catalogmodel.ProductDetail, error) {
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
		if err := json.Unmarshal(sku.Attributes, &attributes); err != nil {
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
			Url:  b.common.MustGetFileURL(ctx, res.Provider, res.ObjectKey),
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

	priceMap, err := b.promotion.CalculatePromotedPrices(ctx, skus, map[int64]db.CatalogProductSpu{
		spu.ID: spu,
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

	return catalogmodel.ProductDetail{
		ID:          spu.ID,
		Code:        spu.Code,
		VendorID:    spu.AccountID,
		Name:        spu.Name,
		Description: spu.Description,
		Brand:       b.mustGetBrand(ctx, spu.BrandID),
		IsActive:    spu.IsActive,
		Category:    b.mustGetCategory(ctx, spu.CategoryID),
		Rating: catalogmodel.ProductRating{
			Score:     rating.Score / 2, // convert 10 scale to 5 scale
			Total:     rating.Count,
			Breakdown: ratingBreakdown,
		},
		Resources:      slice.EnsureSlice(resourceMap[spu.ID]),
		Promotions:     slice.EnsureSlice(promotions),
		Skus:           skusDetail,
		Specifications: nil,
	}, nil
}
