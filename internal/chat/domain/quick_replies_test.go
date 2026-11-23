package domain_test

import (
	"slices"
	"testing"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/locale/localetest"
)

// Every key the code writes is in text/<tag>.json.
func TestTextKeys(t *testing.T) {
	t.Parallel()
	localetest.Keys(t, domain.Words, ".", map[string]int{"say": 0})
}

// The defaults read in the language asked, Vietnamese without one.
func TestDefaultQuickReplies(t *testing.T) {
	t.Parallel()
	vi, en := domain.DefaultQuickReplies(locale.Vietnamese), domain.DefaultQuickReplies(locale.English)
	if len(vi) != 3 || vi[0] != "Còn bạn ơi." || en[0] != "Yes, still available." || slices.Equal(vi, en) {
		t.Fatalf("vi %q en %q", vi, en)
	}
	if !slices.Equal(domain.DefaultQuickReplies(""), vi) {
		t.Fatal("no tag")
	}
}
