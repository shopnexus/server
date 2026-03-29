package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/google/uuid"
	"github.com/gosimple/slug"
	null "github.com/guregu/null/v6"

	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
)

func upsertCategory(ctx context.Context, store *catalogdb.Queries, breadcrumb []string) (uuid.UUID, error) {
	leaf := "General"
	if len(breadcrumb) > 0 {
		leaf = breadcrumb[len(breadcrumb)-1]
	}

	category, err := store.GetCategory(ctx, catalogdb.GetCategoryParams{
		Name: null.StringFrom(leaf),
	})
	if err == nil {
		return category.ID, nil
	}

	category, err = store.CreateDefaultCategory(ctx, catalogdb.CreateDefaultCategoryParams{
		Name:        leaf,
		Description: leaf,
		ParentID:    null.Int{},
	})
	if err != nil {
		return uuid.Nil, err
	}

	return category.ID, nil
}

func createTags(
	ctx context.Context,
	store *catalogdb.Queries,
	spuID uuid.UUID,
	input InputProduct,
) error {
	tagSet := make(map[string]bool)

	for _, crumb := range input.Breadcrumb {
		tagID := slug.Make(crumb)
		if tagID != "" && len(tagID) <= 100 {
			tagSet[tagID] = true
		}
	}

	if input.Brand != "" {
		tagID := slug.Make(input.Brand)
		if tagID != "" && len(tagID) <= 100 {
			tagSet[tagID] = true
		}
	}

	tagSpecs := []string{"Estilo", "Material", "Temporada", "Style", "Season"}
	for _, spec := range input.ProductSpecifications {
		for _, tagSpec := range tagSpecs {
			if spec.Name == tagSpec && spec.Value != "" {
				tagID := slug.Make(spec.Value)
				if tagID != "" && len(tagID) <= 100 {
					tagSet[tagID] = true
				}
			}
		}
	}

	for tagID := range tagSet {
		_, err := store.GetTag(ctx, null.StringFrom(tagID))
		if err != nil {
			_, err = store.CreateTag(ctx, catalogdb.CreateTagParams{
				ID:          tagID,
				Description: null.StringFrom(tagID),
			})
			if err != nil {
				continue
			}
		}

		_, err = store.CreateProductSpuTag(ctx, catalogdb.CreateProductSpuTagParams{
			SpuID: spuID,
			Tag:   tagID,
		})
		if err != nil {
			continue
		}
	}

	return nil
}

// generateVariationCombinations generates all combinations of variation options
func generateVariationCombinations(variations []Variation) [][]map[string]string {
	if len(variations) == 0 {
		return nil
	}

	var validVariations []Variation
	for _, v := range variations {
		if len(v.Variations) > 0 && v.Name != "" {
			validVariations = append(validVariations, v)
		}
	}

	if len(validVariations) == 0 {
		return nil
	}

	total := 1
	for _, v := range validVariations {
		total *= len(v.Variations)
	}

	if total > 20 {
		for i := range validVariations {
			if len(validVariations[i].Variations) > 2 {
				validVariations[i].Variations = validVariations[i].Variations[:2]
			}
		}
	}

	var result [][]map[string]string
	var generate func(depth int, current []map[string]string)
	generate = func(depth int, current []map[string]string) {
		if depth == len(validVariations) {
			combo := make([]map[string]string, len(current))
			copy(combo, current)
			result = append(result, combo)
			return
		}

		v := validVariations[depth]
		for _, opt := range v.Variations {
			attr := map[string]string{
				"name":  v.Name,
				"value": opt,
			}
			generate(depth+1, append(current, attr))
		}
	}

	generate(0, nil)
	return result
}

var stockRegex = regexp.MustCompile(`(?i)existencias|stock`)

func pickCurrentStock(input InputProduct) int64 {
	if input.Stock != nil {
		switch v := input.Stock.(type) {
		case float64:
			return int64(v)
		case string:
			return toBigInt(v)
		default:
			return 0
		}
	}

	for _, spec := range input.ProductSpecifications {
		if stockRegex.MatchString(spec.Name) {
			return toBigInt(spec.Value)
		}
	}

	return 0
}

func toBigInt(value any) int64 {
	if value == nil {
		return 0
	}

	var str string
	switch v := value.(type) {
	case string:
		str = v
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		str = fmt.Sprintf("%v", v)
	}

	re := regexp.MustCompile(`[^0-9.-]`)
	str = re.ReplaceAllString(str, "")

	num, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0
	}

	return int64(num)
}
