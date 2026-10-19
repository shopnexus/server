package accounttest_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

func TestMemStore(t *testing.T) {
	accounttest.RunStoreContract(t, func(t *testing.T) (account.Store, postgres.Querier) {
		return accounttest.NewMemStore(), nil
	})
}
