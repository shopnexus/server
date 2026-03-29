package main

// InputProduct represents the structure of products from data.json
type InputProduct struct {
	URL                            string            `json:"url"`
	ID                             int64             `json:"id"`
	Title                          string            `json:"title"`
	Sold                           int64             `json:"sold"`
	Rating                         float64           `json:"rating"`
	Reviews                        int64             `json:"reviews"`
	InitialPrice                   float64           `json:"initial_price"`
	FinalPrice                     float64           `json:"final_price"`
	Currency                       string            `json:"currency"`
	Stock                          any               `json:"stock"`
	Favorite                       int64             `json:"favorite"`
	Image                          []string          `json:"image"`
	Video                          *string           `json:"video"`
	SellerName                     string            `json:"seller_name"`
	ShopURL                        string            `json:"shop_url"`
	Breadcrumb                     []string          `json:"breadcrumb"`
	ProductSpecifications          []Spec            `json:"Product Specifications"`
	ProductDescription             string            `json:"Product Description"`
	SellerRating                   float64           `json:"seller_rating"`
	SellerProducts                 int64             `json:"seller_products"`
	SellerChatsRespondedPercentage float64           `json:"seller_chats_responded_percentage"`
	SellerChatTimeReply            string            `json:"seller_chat_time_reply"`
	SellerJoinedDate               string            `json:"seller_joined_date"`
	SellerFollowers                int64             `json:"seller_followers"`
	Variations                     []Variation       `json:"variations"`
	Domain                         string            `json:"domain"`
	Brand                          string            `json:"brand"`
	CategoryID                     int64             `json:"category_id"`
	FlashSale                      bool              `json:"flash_sale"`
	FlashSaleTime                  string            `json:"flash_sale_time"`
	ProductVariation               []VariationOption `json:"product_variation"`
	GMVCal                         float64           `json:"gmv_cal"`
	CategoryURL                    string            `json:"category_url"`
	Vouchers                       []Voucher         `json:"vouchers"`
	IsAvailable                    string            `json:"is_available"`
	SellerID                       string            `json:"seller_id"`
	ProductRatings                 any               `json:"product_ratings"`
}

type Spec struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Variation struct {
	Name       string   `json:"name"`
	Variations []string `json:"variations"`
}

type VariationOption struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Voucher struct {
	Claimable          bool     `json:"claimable"`
	Currency           string   `json:"currency"`
	Discount           *float64 `json:"discount"`
	DiscountCap        float64  `json:"discount_cap"`
	DiscountPercentage float64  `json:"discount_percentage"`
	MinSpend           float64  `json:"min_spend"`
	ShopVoucher        string   `json:"shop_vouncher"`
	SpecialCondition   *string  `json:"special_condition"`
	TextInfo           string   `json:"text_info"`
	ValidityEnd        string   `json:"validity_end"`
	ValidityStart      string   `json:"validity_start"`
}
