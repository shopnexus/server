package domain

import (
	"embed"
	"strings"

	"github.com/shopnexus/server/internal/platform/locale"
)

//go:embed text/*.json
var textFiles embed.FS

// words is chat's text, the default quick replies, in every language:
// text/<tag>.json.
var words = locale.MustLoad(textFiles, "text")

// DefaultQuickReplies are the quick replies of an account that never saved
// any, in t: answers a seller often gives.
func DefaultQuickReplies(t locale.Tag) []string {
	say := func(key string) string { return words.Say(locale.Of(string(t)), key) }
	return []string{say("reply.still_available"), say("reply.viewing"), say("reply.price")}
}

// TrimQuickReplies returns each answer without the spaces around it, in the
// same order.
func TrimQuickReplies(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = strings.TrimSpace(s)
	}
	return out
}
