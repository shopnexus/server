package ordermodel

import "fmt"

// SummarizeNames returns a human-readable summary from a list of names.
// e.g. "iPhone 15 Pro", "iPhone 15 Pro and 1 more"
func SummarizeNames(names []string) string {
	switch len(names) {
	case 0:
		return "your items"
	case 1:
		return names[0]
	default:
		return fmt.Sprintf("%s and %d more", names[0], len(names)-1)
	}
}

// SummarizeItems returns a human-readable summary of item names.
func SummarizeItems(items []OrderItem) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item.SkuName != "" {
			names = append(names, item.SkuName)
		}
	}
	return SummarizeNames(names)
}
