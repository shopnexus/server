package service

import (
	"strings"
	"testing"
)

func TestSystemPrompt(t *testing.T) {
	t.Parallel()
	if strings.Contains(systemPrompt, "category_code") {
		t.Error("the model is asked for a category; catalog guesses it")
	}
	// The rule of app/mohinh.md section 1: the model names no mark.
	if !strings.Contains(systemPrompt, "Never name a mark, scratch, dent, crack, stain or fault the seller did not name") {
		t.Error("the marks rule is missing")
	}
}

func TestUserPrompt(t *testing.T) {
	t.Parallel()
	p := userPrompt("mình bán bốn triệu rưỡi có thương lượng", 8, fromServer)
	if !strings.Contains(p, "8 frames, attached below in time order") || !strings.Contains(p, "from our speech recognition") ||
		!strings.Contains(p, "<transcript>\nmình bán bốn triệu rưỡi có thương lượng\n</transcript>") {
		t.Fatalf("%s", p)
	}
	if p = userPrompt("Mình bán 4 triệu rưỡi.", 2, fromPhone); !strings.Contains(p, "from the seller's phone") {
		t.Fatalf("%s", p)
	}
	if p = userPrompt("  ", 1, fromNone); !strings.Contains(p, "1 frame, attached below") || !strings.Contains(p, "<transcript>\n</transcript>") ||
		!strings.Contains(p, "The seller said nothing") {
		t.Fatalf("%s", p)
	}
}
