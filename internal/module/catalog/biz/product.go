package catalogbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	"shopnexus-remastered/internal/utils/pgutil"
)

type GetProductDetailParams struct {
	ID int64
}

func (c *CatalogBiz) GetProductDetail(ctx context.Context, params GetProductDetailParams) (catalogmodel.ProductDetail, error) {
	var zero catalogmodel.ProductDetail
	spu, err := c.storage.GetCatalogProductSpu(ctx, db.GetCatalogProductSpuParams{
		ID: pgutil.Int64ToPgInt8(params.ID),
	})
	if err != nil {
		return zero, err
	}

	var skuIDs []int64
	var skusDetail []catalogmodel.SkuDetail
	skus, err := c.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		SpuID: []int64{spu.ID},
	})
	if err != nil {
		return zero, err
	}

	for _, sku := range skus {
		skuIDs = append(skuIDs, sku.ID)
	}

	// Get attributes for each SKU
	attributes, err := c.storage.ListCatalogProductSkuAttribute(ctx, db.ListCatalogProductSkuAttributeParams{
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
		skusDetail = append(skusDetail, catalogmodel.SkuDetail{
			ID:            sku.ID,
			Price:         sku.Price,
			OriginalPrice: sku.Price,
			Attributes:    attrMap[sku.ID],
		})
	}

	// Get images
	resources, err := c.storage.ListSharedResource(ctx, db.ListSharedResourceParams{
		OwnerType: []db.SharedResourceType{db.SharedResourceTypeProductSpu},
		OwnerID:   []int64{spu.ID},
	})
	if err != nil {
		return zero, err
	}
	var images []string
	for _, r := range resources {
		images = append(images, r.Url)
	}

	// get rating
	rating, err := c.storage.DetailRating(ctx, db.DetailRatingParams{
		RefType: db.CatalogCommentRefTypeProductSPU,
		RefID:   spu.ID,
	})
	ratingBreakdown := make(map[int]int)
	if err != nil {
		return zero, err
	}
	ratingBreakdown[5] = int(rating.FiveCount)
	ratingBreakdown[4] = int(rating.FourCount)
	ratingBreakdown[3] = int(rating.ThreeCount)
	ratingBreakdown[2] = int(rating.TwoCount)
	ratingBreakdown[1] = int(rating.OneCount)

	category, err := c.storage.GetCatalogCategory(ctx, db.GetCatalogCategoryParams{
		ID: pgutil.Int64ToPgInt8(spu.CategoryID),
	})
	if err != nil {
		return zero, err
	}

	// Get sold count from inventory
	inventories, err := c.storage.ListInventoryStock(ctx, db.ListInventoryStockParams{
		RefType: []db.InventoryStockType{db.InventoryStockTypeProductSku},
		RefID:   skuIDs,
	})
	if err != nil {
		return zero, err
	}
	sold := 0
	for _, inv := range inventories {
		sold += int(inv.Sold)
	}

	priceMap, err := c.promotionBiz.CalculatePromotedPrices(ctx, skus, map[int64]*db.CatalogProductSpu{
		spu.ID: &spu,
	})
	if err != nil {
		return zero, err
	}
	promoSet := make(map[int64]struct{})
	promotions := []catalogmodel.ProductCardPromo{}
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
		Name:        spu.Name,
		Description: spu.Description,
		Images:      images,
		Category:    category.Name,
		Rating: catalogmodel.RatingDetail{
			Score:     rating.Score / 2, // convert 10 scale to 5 scale
			Total:     rating.Count,
			Breakdown: ratingBreakdown,
		},
		Sold:           sold,
		Promotions:     promotions,
		Skus:           skusDetail,
		Specifications: nil,
	}, nil
}
