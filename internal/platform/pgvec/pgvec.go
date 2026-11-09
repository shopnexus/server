// Package pgvec writes and reads pgvector values in their text form,
// "[0.5,-0.25,1]", which vector and halfvec cast from and print. Queries pass
// a vector as text and cast it in SQL, so no driver type is registered.
package pgvec

import (
	"fmt"
	"strconv"
	"strings"
)

// Format writes v in pgvector's text form. Its values must be finite; the
// embed client refuses anything else before a vector gets here.
func Format(v []float32) string {
	var b strings.Builder
	b.Grow(len(v)*11 + 2)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// Parse reads pgvector's text form.
func Parse(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("pgvec: not a vector: %.20q", s)
	}
	body := s[1 : len(s)-1]
	if body == "" {
		return []float32{}, nil
	}
	parts := strings.Split(body, ",")
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("pgvec: element %d: %w", i, err)
		}
		out[i] = float32(f)
	}
	return out, nil
}
