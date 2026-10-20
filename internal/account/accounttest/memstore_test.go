package accounttest_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
)

func TestMemStore(t *testing.T) {
	accounttest.RunStoreContract(t, func(*testing.T) account.Store { return accounttest.NewMemStore() })
}
