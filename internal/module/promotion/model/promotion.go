package promotionmodel

import (
	promotiondb "shopnexus-remastered/internal/module/promotion/db"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type Promotion struct {
	ID      uuid.UUID     `json:"id"`
	Code    string        `json:"code"`
	OwnerID uuid.NullUUID `json:"owner_id"`

	Type        promotiondb.PromotionType `json:"type"`
	Title       string                    `json:"title"`
	Description null.String               `json:"description"`
	IsActive    bool                      `json:"is_active"`
	AutoApply   bool                      `json:"auto_apply"`

	DateStarted time.Time `json:"date_started"`
	DateEnded   null.Time `json:"date_ended"`

	DateCreated time.Time `json:"date_created"`
	DateUpdated time.Time `json:"date_updated"`

	Refs []PromotionRef `json:"refs"`
}

type PromotionRef struct {
	RefType promotiondb.PromotionRefType `validate:"required,validateFn=Valid"`
	RefID   uuid.UUID                    `validate:"required"`
}

type PromotionDiscount struct {
	Promotion
	MinSpend        sharedmodel.Concurrency     `json:"min_spend"`
	MaxDiscount     sharedmodel.Concurrency     `json:"max_discount"`
	DiscountPercent null.Float                  `json:"discount_percent"`
	DiscountPrice   sharedmodel.NullConcurrency `json:"discount_price"`
}
