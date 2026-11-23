package domain

import (
	"embed"

	"github.com/shopnexus/server/internal/platform/locale"
)

//go:embed text/*.json
var textFiles embed.FS

// words is trust's text, the scam signals, in every language:
// text/<tag>.json.
var words = locale.MustLoad(textFiles, "text")

func say(t locale.Tag, key string, args ...any) string {
	return words.Say(locale.Of(string(t)), key, args...)
}
