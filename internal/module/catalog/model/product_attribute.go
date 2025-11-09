package catalogmodel

type ProductAttribute struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Add more fields like referenced resource (for jumping across different thumbnails for each sku)
}

type ProductSpecification struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
