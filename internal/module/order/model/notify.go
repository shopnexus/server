package ordermodel

import "fmt"

// SummarizeItems returns a human-readable summary of item names.
// e.g. "iPhone 15 Pro", "iPhone 15 Pro, Galaxy S24", "iPhone 15 Pro, Galaxy S24 and 1 more"
func SummarizeItems(items []OrderItem) string {
	if len(items) == 0 {
		return "your items"
	}
	if len(items) == 1 {
		return items[0].SkuName
	}
	if len(items) == 2 {
		return items[0].SkuName + ", " + items[1].SkuName
	}
	return fmt.Sprintf("%s, %s and %d more", items[0].SkuName, items[1].SkuName, len(items)-2)
}
