package catalogecho

import (
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	biz *catalogbiz.CatalogBiz
}

func NewHandler(e *echo.Echo, catalogbiz *catalogbiz.CatalogBiz) *Handler {
	h := &Handler{biz: catalogbiz}
	api := e.Group("/api/v1/catalog")

	// Friendly APIs
	api.GET("/product-detail", h.GetProductDetail)
	api.GET("/product-card", h.ListProductCard)
	api.GET("/product-card/recommended", h.ListRecommendedProductCard)

	// Product Spu
	spuApi := api.Group("/product-spu")
	spuApi.GET("", h.ListProductSpu)
	spuApi.GET("/:id", h.GetProductSpu)
	spuApi.POST("", h.CreateProductSpu)
	spuApi.PATCH("", h.UpdateProductSpu)
	spuApi.DELETE("/:id", h.DeleteProductSpu)

	// Product Sku
	skuApi := api.Group("/product-sku")
	skuApi.GET("", h.ListProductSku)
	skuApi.POST("", h.CreateProductSku)
	skuApi.PATCH("", h.UpdateProductSku)
	skuApi.DELETE("", h.DeleteProductSku)

	// Comment
	commentApi := api.Group("/comment")
	commentApi.GET("", h.ListComment)
	commentApi.POST("", h.CreateComment)
	commentApi.PATCH("", h.UpdateComment)
	commentApi.DELETE("", h.DeleteComment)

	// Tag
	tagApi := api.Group("/tag")
	tagApi.GET("", h.ListTag)
	tagApi.GET("/:tag", h.GetTag)

	// Brand
	brandApi := api.Group("/brand")
	brandApi.GET("", h.ListBrand)

	// Category
	categoryApi := api.Group("/category")
	categoryApi.GET("", h.ListCategory)

	return h
}
