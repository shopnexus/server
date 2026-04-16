package analyticconfig

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

func (w PopularityWeights) WeightMap() map[string]float64 {
	return map[string]float64{
		"purchase":                  w.Purchase,
		"add_to_cart":               w.AddToCart,
		"view":                      w.View,
		"add_to_favorites":          w.AddToFavorites,
		"write_review":              w.WriteReview,
		"rating_high":               w.RatingHigh,
		"rating_medium":             w.RatingMedium,
		"ask_question":              w.AskQuestion,
		"click_from_search":         w.ClickFromSearch,
		"click_from_recommendation": w.ClickFromRecommend,
		"click_from_category":       w.ClickFromCategory,
		"view_similar_products":     w.ViewSimilarProducts,
		"product_impression":        w.ProductImpression,
		"checkout_started":          w.CheckoutStarted,
		"remove_from_cart":          w.RemoveFromCart,
		"return_product":            w.ReturnProduct,
		"refund_requested":          w.RefundRequested,
		"cancel_order":              w.CancelOrder,
		"rating_low":                w.RatingLow,
		"report_product":            w.ReportProduct,
		"dislike":                   w.Dislike,
		"hide_item":                 w.HideItem,
		"not_interested":            w.NotInterested,
		"view_bounce":               w.ViewBounce,
	}
}
