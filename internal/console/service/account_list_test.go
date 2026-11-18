package service_test

import (
	"context"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/page"
)

// noAccounts is module account's Service with no accounts, as a remote
// client would answer.
type noAccounts struct{ accountsvc.Service }

func (noAccounts) ConsoleAccount(context.Context, uuid.UUID) (accountsvc.ConsoleAccount, error) {
	return accountsvc.ConsoleAccount{}, accountdomain.ErrAccountNotFound
}

// An ID that names no account gives an empty page, whatever implements
// account's Service.
func TestListAccountsByAnUnknownID(t *testing.T) {
	t.Parallel()
	s := service.New(service.Deps{Accounts: noAccounts{}})
	out, err := s.ListAccounts(context.Background(), service.AccountQuery{Q: uuid.NewV7().String()}, page.Request{Limit: 10})
	if err != nil || len(out.Items) != 0 || out.Total != nil {
		t.Fatalf("%+v %v", out, err)
	}
}

// oneAccount is module account's Service with one account, in status, among
// three suspended ones.
type oneAccount struct {
	accountsvc.Service

	status accountdomain.Status
}

func (a oneAccount) ConsoleAccount(_ context.Context, id uuid.UUID) (accountsvc.ConsoleAccount, error) {
	return accountsvc.ConsoleAccount{ID: id, Status: a.status}, nil
}

func (oneAccount) SuspendedCount(context.Context) (int, error) { return 3, nil }

// With status and q, the account q names is listed only if it is suspended,
// and the count is of every suspended account.
func TestListSuspendedAccountsByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	q := service.AccountQuery{Q: uuid.NewV7().String(), Suspended: true}
	out, err := service.New(service.Deps{Accounts: oneAccount{status: accountdomain.StatusActive}}).ListAccounts(ctx, q, page.Request{})
	if err != nil || len(out.Items) != 0 || out.Total == nil || *out.Total != 3 {
		t.Fatalf("an active account: %+v %v", out, err)
	}
	out, err = service.New(service.Deps{Accounts: oneAccount{status: accountdomain.StatusSuspended}}).ListAccounts(ctx, q, page.Request{})
	if err != nil || len(out.Items) != 1 || out.Items[0].ID.String() != q.Q || out.Total == nil || *out.Total != 3 {
		t.Fatalf("a suspended account: %+v %v", out, err)
	}
}
