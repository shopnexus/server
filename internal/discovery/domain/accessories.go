package domain

import (
	"slices"
	"strings"
	"unicode"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/textkey"
)

// accessoryCategories are the subcategories of a device's accessories. Their
// titles list every model they fit, so on raw words they outrank the device.
var accessoryCategories = []string{"phones.accessories", "camera.accessories"}

// accessoryCues are words, beside the category's keywords, that people type
// for an accessory.
var accessoryCues = map[string][]string{
	"phones.accessories": {"ốp", "case", "sạc", "cáp", "miếng dán", "phụ kiện"},
	"camera.accessories": {"tripod", "chân máy", "phụ kiện"},
}

// AccessoryWeight is how much an accessory sinks on raw words: the gap the
// model opens between a device it boosts and an accessory it demotes.
var AccessoryWeight = -2 * weight[AttrCategory]

// AccessoryDemotes are the boosts of a search on raw words no model read:
// each device-accessory category the query names no accessory of sinks by
// AccessoryWeight. A search filtered to a subcategory gets none, and one
// filtered to a root only its own.
func AccessoryDemotes(q string, f catalogdomain.Filters) []catalogdomain.Boost {
	if f.CategoryCode != "" {
		return nil
	}
	query := wordKey(q)
	var out []catalogdomain.Boost
	for _, code := range accessoryCategories {
		if f.CategoryRoot != "" && !strings.HasPrefix(code, f.CategoryRoot+".") {
			continue
		}
		named := false
		for _, cue := range slices.Concat(catalogdomain.CategoryKeywords[code], accessoryCues[code]) {
			if strings.Contains(query, wordKey(cue)) {
				named = true
				break
			}
		}
		if !named {
			out = append(out, catalogdomain.Boost{Kind: catalogdomain.BoostCategory, Code: code, Weight: AccessoryWeight})
		}
	}
	return out
}

// wordKey is s folded, its words split on anything but letters and digits,
// with a space either side so a cue matches whole words.
func wordKey(s string) string {
	words := strings.FieldsFunc(textkey.Fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return " " + strings.Join(words, " ") + " "
}
