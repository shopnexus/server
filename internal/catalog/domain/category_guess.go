package domain

import (
	"strings"
	"unicode/utf8"
)

// CategoryPrototype is one text of a subcategory as a unit vector.
type CategoryPrototype struct {
	Code   string
	Vector []float32
}

// GuessTexts are the texts a guess compares with, each with its code: per
// subcategory with keywords, its "group › name" and that name with each
// keyword. Measured on 86 seller listings (2026-11-19), the nearest of these
// gives the seller's category for 92% of title-and-description texts.
func GuessTexts(tree []Category) (texts, codes []string) {
	names := make(map[string]string, len(tree))
	for _, c := range tree {
		names[c.Code] = c.DisplayName
	}
	for _, c := range tree {
		words := CategoryKeywords[c.Code]
		if c.ParentCode == "" || len(words) == 0 {
			continue
		}
		name := names[c.ParentCode] + " › " + c.DisplayName
		texts, codes = append(texts, name), append(codes, c.Code)
		for _, w := range words {
			texts, codes = append(texts, name+": "+w), append(codes, c.Code)
		}
	}
	return texts, codes
}

// GuessText is what a guess embeds of a listing: its title, then its
// description, cut as EmbeddingText is; "" when both are blank.
func GuessText(title, description string) string {
	text := strings.TrimSpace(strings.TrimSpace(title) + "\n" + strings.TrimSpace(description))
	if len(text) > EmbeddingTextMax {
		text = text[:EmbeddingTextMax]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return text
}

// GuessCategory is the code of the prototype nearest to text, a unit
// vector in the same space; "" without prototypes.
func GuessCategory(text []float32, protos []CategoryPrototype) string {
	best, score := "", -2.0
	for _, p := range protos {
		if s := dot(text, p.Vector); s > score {
			best, score = p.Code, s
		}
	}
	return best
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range min(len(a), len(b)) {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}
