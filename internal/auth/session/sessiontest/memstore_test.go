package sessiontest_test

import (
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/auth/session/sessiontest"
)

func TestMemStore(t *testing.T) {
	sessiontest.RunStoreContract(t, func(*testing.T) (session.Store, func() uuid.UUID) {
		return sessiontest.NewMemStore(), uuid.NewV7
	})
}
