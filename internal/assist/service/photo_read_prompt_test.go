package service

import (
	"strings"
	"testing"
)

func TestPhotoSystemPrompt(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"at most 300 characters",
		"Never name a mark, scratch, dent, crack, stain or fault",
		"no age, use, repairs, battery health",
	} {
		if !strings.Contains(photoSystemPrompt, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(photoSystemPrompt, "category_code") {
		t.Error("the model is asked for a category; catalog guesses it")
	}
}
