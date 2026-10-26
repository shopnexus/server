package domain

// Category is a node of the two-level category tree; a listing takes a
// level-2 category, one with a parent.
type Category struct {
	Code        string `json:"code"`
	ParentCode  string `json:"parent_code,omitempty"`
	DisplayName string `json:"display_name"`
}

// Province is a province or centrally governed city.
type Province struct {
	Code        string `json:"code"`
	DisplayName string `json:"display_name"`
}

// Commune is a commune, ward or special zone, with a point inside it.
type Commune struct {
	Code         string  `json:"code"`
	ProvinceCode string  `json:"province_code"`
	DisplayName  string  `json:"display_name"`
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
}
