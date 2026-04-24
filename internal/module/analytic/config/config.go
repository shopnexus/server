package analyticconfig

import (
	"reflect"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
)

type Config struct {
}

type PopularityWeights struct {
	Purchase            float64 `yaml:"purchase"                  mapstructure:"purchase"`
	AddToCart           float64 `yaml:"add_to_cart"               mapstructure:"add_to_cart"`
	View                float64 `yaml:"view"                      mapstructure:"view"`
	AddToFavorites      float64 `yaml:"add_to_favorites"          mapstructure:"add_to_favorites"`
	WriteReview         float64 `yaml:"write_review"              mapstructure:"write_review"`
	RatingHigh          float64 `yaml:"rating_high"               mapstructure:"rating_high"`
	RatingMedium        float64 `yaml:"rating_medium"             mapstructure:"rating_medium"`
	AskQuestion         float64 `yaml:"ask_question"              mapstructure:"ask_question"`
	ClickFromSearch     float64 `yaml:"click_from_search"         mapstructure:"click_from_search"`
	ClickFromRecommend  float64 `yaml:"click_from_recommendation" mapstructure:"click_from_recommendation"`
	ClickFromCategory   float64 `yaml:"click_from_category"       mapstructure:"click_from_category"`
	ViewSimilarProducts float64 `yaml:"view_similar_products"     mapstructure:"view_similar_products"`
	ProductImpression   float64 `yaml:"product_impression"        mapstructure:"product_impression"`
	CheckoutStarted     float64 `yaml:"checkout_started"          mapstructure:"checkout_started"`
	RemoveFromCart      float64 `yaml:"remove_from_cart"          mapstructure:"remove_from_cart"`
	ReturnProduct       float64 `yaml:"return_product"            mapstructure:"return_product"`
	RefundRequested     float64 `yaml:"refund_requested"          mapstructure:"refund_requested"`
	CancelOrder         float64 `yaml:"cancel_order"              mapstructure:"cancel_order"`
	RatingLow           float64 `yaml:"rating_low"                mapstructure:"rating_low"`
	ReportProduct       float64 `yaml:"report_product"            mapstructure:"report_product"`
	Dislike             float64 `yaml:"dislike"                   mapstructure:"dislike"`
	HideItem            float64 `yaml:"hide_item"                 mapstructure:"hide_item"`
	NotInterested       float64 `yaml:"not_interested"            mapstructure:"not_interested"`
	ViewBounce          float64 `yaml:"view_bounce"               mapstructure:"view_bounce"`
}

func (w PopularityWeights) WeightMap() map[analyticmodel.Event]float64 {
	result := make(map[analyticmodel.Event]float64)

	v := reflect.ValueOf(w)
	t := v.Type()

	for i := range t.NumField() {
		field := t.Field(i)

		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}

		result[analyticmodel.Event(tag)] = v.Field(i).Float()
	}

	return result
}

func DefaultPopularityWeights() PopularityWeights {
	return PopularityWeights{
		Purchase:            0.8,
		AddToCart:           0.5,
		View:                0.3,
		AddToFavorites:      0.6,
		WriteReview:         0.5,
		RatingHigh:          0.4,
		RatingMedium:        0.1,
		AskQuestion:         0.25,
		ClickFromSearch:     0.2,
		ClickFromRecommend:  0.15,
		ClickFromCategory:   0.12,
		ViewSimilarProducts: 0.15,
		ProductImpression:   0.1,
		CheckoutStarted:     0.6,
		RemoveFromCart:      -0.3,
		ReturnProduct:       -0.6,
		RefundRequested:     -0.7,
		CancelOrder:         -0.6,
		RatingLow:           -0.5,
		ReportProduct:       -1.2,
		Dislike:             -0.5,
		HideItem:            -0.35,
		NotInterested:       -0.3,
		ViewBounce:          -0.1,
	}
}
