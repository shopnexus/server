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
	var b strings.Builder
	b.WriteString(l.title)
	b.WriteString("\n" + category)
	b.WriteString("\n" + conditionWords[l.condition])
	for _, s := range l.specs {
		b.WriteString("\n" + s.Label + ": " + s.Value)
	}
	if l.description != "" {
		b.WriteString("\n" + l.description)
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
