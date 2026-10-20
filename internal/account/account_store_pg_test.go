package account_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

func TestPGStore(t *testing.T) {
	accounttest.RunStoreContract(t, func(t *testing.T) account.Store { return account.NewPGStore(pgtest.New(t)) })
}
