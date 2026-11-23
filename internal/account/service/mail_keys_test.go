package service_test

import (
	"testing"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/locale/localetest"
)

// Every key the code mail writes is in mail/<tag>.json.
func TestMailKeys(t *testing.T) {
	t.Parallel()
	localetest.Keys(t, service.MailWords, ".", map[string]int{"say": 0})
}
