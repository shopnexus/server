package locale_test

import (
	"testing"

	"github.com/shopnexus/server/internal/platform/locale"
)

func TestMatch(t *testing.T) {
	t.Parallel()
	for header, want := range map[string]locale.Tag{
		"": locale.Vietnamese, "en-US,en;q=0.9": locale.English, "vi-VN,vi;q=0.9,en;q=0.8": locale.Vietnamese,
		"fr-FR,en;q=0.5": locale.English, "ja-JP": locale.Vietnamese, "not a header;;": locale.Vietnamese,
	} {
		if got := locale.Match(header); got != want {
			t.Errorf("%q: %s, want %s", header, got, want)
		}
	}
	if locale.Of("en") != locale.English || locale.Of("xx") != locale.Vietnamese {
		t.Error("Of")
	}
}
