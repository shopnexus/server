package service

import (
	"fmt"
	"strings"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// writeCategories ends a read's system prompt with every subcategory, one
// line each as "code — group › name". Roots are left out: a draft takes a
// subcategory only.
func writeCategories(b *strings.Builder, cats []catalogdomain.Category) {
	names := make(map[string]string, len(cats))
	for _, c := range cats {
		names[c.Code] = c.DisplayName
	}
	for _, c := range cats {
		if c.ParentCode == "" {
			continue
		}
		fmt.Fprintf(b, "%s — %s › %s\n", c.Code, names[c.ParentCode], c.DisplayName)
	}
}
