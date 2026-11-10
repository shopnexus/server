package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/support/domain"
)

func TestBody(t *testing.T) {
	t.Parallel()
	if b, err := domain.Body("  Ứng dụng chạy chậm  "); err != nil || b != "Ứng dụng chạy chậm" {
		t.Fatalf("trimmed: %q %v", b, err)
	}
	if _, err := domain.Body(strings.Repeat("ạ", 1000)); err != nil {
		t.Fatalf("1000 characters: %v", err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("ạ", 1001)} {
		if _, err := domain.Body(bad); !errors.Is(err, domain.ErrInvalidFeedback) {
			t.Errorf("%d characters: %v", len([]rune(bad)), err)
		}
	}
}
