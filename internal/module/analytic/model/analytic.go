package analyticmodel

import (
	"encoding/json"
	analyticdb "shopnexus-remastered/internal/module/analytic/db/sqlc"
	"time"

	"github.com/google/uuid"
)

const (
	TopicAnalyticInteraction = "analytic.interaction"

	// High intent events
	EventPurchase    = "purchase"
	EventAddToCart   = "add_to_cart"
	EventView        = "view"
	EventWriteReview = "write_review"
	EventRatingHigh  = "rating_high"
	EventRatingMed   = "rating_medium"
	EventRatingLow   = "rating_low"

	// Discovery events
	EventClickFromSearch     = "click_from_search"
	EventClickFromRecommend  = "click_from_recommendation"
	EventClickFromCategory   = "click_from_category"
	EventViewSimilarProducts = "view_similar_products"

	// Additional signals
	EventAddToFavorites = "add_to_favorites"
	EventAskQuestion    = "ask_question"

	// Negative signals
	EventRemoveFromCart = "remove_from_cart"
	EventCancelOrder    = "cancel_order"
	EventRefundReq      = "refund_requested"
	EventReturnProduct  = "return_product"
	EventReportProduct  = "report_product"
	EventDislike        = "dislike"
	EventHideItem       = "hide_item"
	EventNotInterested  = "not_interested"
	EventViewBounce     = "view_bounce"
)

type Interaction struct {
	ID            int64                                 `json:"id"`
	AccountID     uuid.NullUUID                         `json:"account_id"`
	AccountNumber int64                                 `json:"account_number"`
	EventType     string                                `json:"event_type"`
	RefType       analyticdb.AnalyticInteractionRefType `json:"ref_type"`
	RefID         string                                `json:"ref_id"`
	Metadata      json.RawMessage                       `json:"metadata"`
	DateCreated   time.Time                             `json:"date_created"`
}
