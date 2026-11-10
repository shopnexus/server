package domain

import (
	"crypto/sha256"
	"strings"
)

const (
	// TextDims and PhotoDims are the lengths of a listing's text and photo
	// vectors: BGE-M3 and SigLIP 2 so400m. Migration 0024 fixes them.
	TextDims  = 1024
	PhotoDims = 1152
	// EmbeddingTextMax is how many characters of a listing's text are
	// embedded; BGE-M3 reads 8,192 tokens, and a listing's sense is in its
	// first lines.
	EmbeddingTextMax = 2000
)

var conditionWords = map[Condition]string{ConditionNew: "Mới", ConditionLikeNew: "Như mới", ConditionUsed: "Đã dùng"}

// EmbeddingText is what the listing's text vector is made from: the title,
// the category's name, the condition, each spec as "label: value" and the
// description, one per line, cut at EmbeddingTextMax characters.
func (l *Listing) EmbeddingText(category string) string {
	return embeddingText(l.title, category, l.condition, l.specs, l.description)
}

// EmbeddingText is the text a listing posted from d would be embedded from,
// trimmed as NewListing trims it; a price sample embeds a draft with it.
func (d Draft) EmbeddingText(category string) string {
	specs := make([]Spec, len(d.Specs))
	for i, s := range d.Specs {
		specs[i] = Spec{Label: strings.TrimSpace(s.Label), Value: strings.TrimSpace(s.Value)}
	}
	return embeddingText(strings.TrimSpace(d.Title), category, d.Condition, specs, strings.TrimSpace(d.Description))
}

func embeddingText(title, category string, condition Condition, specs []Spec, description string) string {
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n" + category)
	b.WriteString("\n" + conditionWords[condition])
	for _, s := range specs {
		b.WriteString("\n" + s.Label + ": " + s.Value)
	}
	if description != "" {
		b.WriteString("\n" + description)
	}
	text := []rune(b.String())
	if len(text) > EmbeddingTextMax {
		text = text[:EmbeddingTextMax]
	}
	return string(text)
}

// TextHash keys a text vector by the model version and the text, so a change
// of either embeds again.
func TextHash(version, text string) []byte {
	h := sha256.Sum256([]byte(version + "\x00" + text))
	return h[:]
}

// ProcessedPhotos are the object keys of the listing's processed photos, in
// order.
func (l *Listing) ProcessedPhotos() []string {
	var out []string
	for _, p := range l.photos {
		if p.Keys.Photo != "" {
			out = append(out, p.Keys.Photo)
		}
	}
	return out
}
