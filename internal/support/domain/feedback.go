// Package domain holds module support's rules: what a feedback may say and
// how many a person sends in a day.
package domain

import (
	"strings"
	"unicode/utf8"
)

const (
	// MaxBodyRunes is the longest feedback, as the app's counter shows.
	MaxBodyRunes = 1000
)

// Body trims raw and checks its length.
func Body(raw string) (string, error) {
	b := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(b); n < 1 || n > MaxBodyRunes {
		return "", ErrInvalidFeedback.WithField("body", "1 to 1000 characters")
	}
	return b, nil
}
