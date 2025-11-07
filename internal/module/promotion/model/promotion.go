package promotionmodel

import (
	"time"

	"shopnexus-remastered/internal/db"

	"github.com/guregu/null/v6"
)

type PromotionBase struct {
	ID      int64      `json:"id"`
	Code    string     `json:"code"`
	OwnerID null.Int64 `json:"owner_id"`

	Type        db.PromotionType `json:"type"`
	Title       string           `json:"title"`
	Description null.String      `json:"description"`
	IsActive    bool             `json:"is_active"`
	AutoApply   bool             `json:"auto_apply"`

	DateStarted time.Time `json:"date_started"`
	DateEnded   null.Time `json:"date_ended"`

	DateCreated time.Time `json:"date_created"`
	DateUpdated time.Time `json:"date_updated"`

	Refs []PromotionRef `json:"refs"`
}

type PromotionRef struct {
	RefType db.PromotionRefType `validate:"required,validateFn=Valid"`
	RefID   int64               `validate:"required"`
}

type PromotionDiscount struct {
	PromotionBase
	OrderWide       bool       `json:"order_wide"`
	MinSpend        int64      `json:"min_spend"`
	MaxDiscount     int64      `json:"max_discount"`
	DiscountPercent null.Int32 `json:"discount_percent"`
	DiscountPrice   null.Int64 `json:"discount_price"`
}
