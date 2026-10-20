package account_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

func TestPGStore(t *testing.T) {
	accounttest.RunStoreContract(t, func(t *testing.T) (account.Store, postgres.Querier) {
		return account.NewPGStore(), pgtest.New(t)
	})
}
