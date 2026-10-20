package account_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

func clock(t time.Time) func() time.Time { return func() time.Time { return t } }

// noTx runs fn without a transaction; fake stores ignore the Querier.
type noTx struct{}

func (noTx) InTx(_ context.Context, fn func(postgres.Querier) error) error { return fn(nil) }

// seed stores an active account named "Linh" and returns it.
func seed(t *testing.T, store *accounttest.MemStore) account.Account {
	t.Helper()
	a, err := account.New("Linh", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Insert(context.Background(), nil, &a); err != nil {
		t.Fatal(err)
	}
	return a
}
