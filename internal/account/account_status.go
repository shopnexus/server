package account

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/postgres"
)

// AccountStatus returns the status in force now. auth calls it at sign-in and
// at refresh.
type AccountStatus struct {
	Store Store
	Now   func() time.Time
}

func (uc AccountStatus) Execute(ctx context.Context, q postgres.Querier, id uuid.UUID) (Status, error) {
	a, err := uc.Store.Get(ctx, q, id)
	if err != nil {
		return "", err
	}
	return a.Status(uc.Now()), nil
}
