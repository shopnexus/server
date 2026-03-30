package analyticmodel

import (
	"time"

	"github.com/google/uuid"
)

type GetSellerDashboardParams struct {
	SellerID    uuid.UUID `json:"seller_id" validate:"required"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	Granularity string    `json:"granularity" validate:"omitempty,oneof=day week month"`
}

type SellerDashboard struct {
	Summary     DashboardSummary      `json:"summary"`
	Charts      DashboardCharts       `json:"charts"`
	TopProducts []DashboardTopProduct `json:"top_products"`
}

type DashboardSummary struct {
	TotalRevenue   int64               `json:"total_revenue"`
	TotalOrders    int64               `json:"total_orders"`
	ItemsSold      int64               `json:"items_sold"`
	AverageRating  float64             `json:"average_rating"`
	PendingActions int64               `json:"pending_actions"`
	Comparison     DashboardComparison `json:"comparison"`
}

type DashboardComparison struct {
	RevenueChange   *float64 `json:"revenue_change"`
	OrdersChange    *float64 `json:"orders_change"`
	ItemsSoldChange *float64 `json:"items_sold_change"`
}

type DashboardCharts struct {
	Revenue []TimeSeriesPoint `json:"revenue"`
	Orders  []TimeSeriesPoint `json:"orders"`
}

type TimeSeriesPoint struct {
	Date  time.Time `json:"date"`
	Value int64     `json:"value"`
}

type DashboardTopProduct struct {
	SkuID     uuid.UUID `json:"sku_id"`
	SkuName   string    `json:"sku_name"`
	SoldCount int64     `json:"sold_count"`
	Revenue   int64     `json:"revenue"`
}
