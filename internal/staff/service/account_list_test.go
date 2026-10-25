package service_test

import (
	"context"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/staff/service"
)

// noAccounts is module account's Service with no accounts, as a remote
// client would answer.
type noAccounts struct{ accountsvc.Service }

func (noAccounts) Record(context.Context, uuid.UUID) (accountsvc.Record, error) {
	return accountsvc.Record{}, accountdomain.ErrAccountNotFound
}

// An ID that names no account gives an empty page, whatever implements
// account's Service.
func TestListAccountsByAnUnknownID(t *testing.T) {
	t.Parallel()
	s := service.New(service.Deps{Accounts: noAccounts{}})
	out, err := s.ListAccounts(context.Background(), uuid.NewV7().String(), page.Request{Limit: 10})
	if err != nil || len(out.Items) != 0 {
		t.Fatalf("%+v %v", out, err)
	}
}
