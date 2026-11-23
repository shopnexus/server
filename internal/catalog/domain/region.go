package domain

import "github.com/shopnexus/server/internal/platform/locale"

// Category is a node of the two-level category tree; a listing takes a
// level-2 category, one with a parent.
type Category struct {
	Code        string `json:"code"`
	ParentCode  string `json:"parent_code,omitempty"`
	DisplayName string `json:"display_name"`
	// Specs are the specs a listing under it must fill, in order; none on a
	// root or a "Khác".
	Specs []CategorySpec `json:"specs"`
	// SpecLabels are the specs' Vietnamese names, for older apps.
	SpecLabels []string `json:"spec_labels"`
}

// CategorySpec is a spec a category asks for, named in one language.
type CategorySpec struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// SpecNames is a spec a category asks for, named in every language.
type SpecNames struct {
	Key   string                `json:"key"`
	Names map[locale.Tag]string `json:"names"`
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
