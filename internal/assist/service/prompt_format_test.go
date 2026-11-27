package service

import (
	"strings"
	"testing"
)

// Each read's model answers the JSON reply, with a reason when it finds no
// item, and is asked for nothing else.
func TestPromptsAskForTheReply(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]string{"video": systemPrompt, "photo": photoSystemPrompt, "ad": adSystemPrompt} {
		for _, want := range []string{`"subject"`, `"found"`, `"title" and "description"`, `"reason"`, "Never ask the seller for more details"} {
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
