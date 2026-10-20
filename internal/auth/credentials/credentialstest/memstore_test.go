package credentialstest_test

import (
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/credentials/credentialstest"
)

func TestMemStore(t *testing.T) {
	credentialstest.RunStoreContract(t, func(*testing.T) (credentials.Store, func() uuid.UUID) {
		return credentialstest.NewMemStore(), uuid.NewV7
	})
}
