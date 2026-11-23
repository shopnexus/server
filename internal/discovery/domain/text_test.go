package domain_test

import (
	"testing"

	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/locale/localetest"
)

// Every key the code writes is in text/<tag>.json.
func TestTextKeys(t *testing.T) {
	t.Parallel()
	localetest.Keys(t, domain.Words, ".", map[string]int{"say": 0})
}
