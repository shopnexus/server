package credentials_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/credentials/credentialstest"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

func TestPGStore(t *testing.T) {
	credentialstest.RunStoreContract(t, func(t *testing.T) (credentials.Store, func() uuid.UUID) {
		pool := pgtest.New(t)
		newAccount := func() uuid.UUID {
			id := uuid.NewV7()
			if _, err := pool.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES ($1, $1)`, id); err != nil {
				t.Fatal(err)
			}
			return id
		}
		return credentials.NewPGStore(pool), newAccount
	})
}
