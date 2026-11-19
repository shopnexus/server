package service

import (
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/assist/domain"
)

// Each read's model writes the title line, then the description, or
// NO_ITEM, and is asked for nothing else.
func TestPromptsAskForTextOnly(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]string{"video": systemPrompt, "photo": photoSystemPrompt, "link": linkSystemPrompt} {
		for _, want := range []string{"The first line is the title.", "reply with exactly " + domain.NoItem} {
			if !strings.Contains(p, want) {
				t.Errorf("%s: no %q", name, want)
			}
		}
		for _, field := range []string{"category_code", "condition", "specs", "estimate", "spoken_price", "negotiable", "kept"} {
			if strings.Contains(p, field) {
				t.Errorf("%s asks for %s", name, field)
			}
		}
	}
}
