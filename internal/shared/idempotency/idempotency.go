package idempotency

import (
	"context"
	"errors"

	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
)

// Keys pairs a forward-action key with its compensator key.
type Keys struct {
	ClaimKey   uuid.UUID
	ConsumeKey uuid.UUID
}

type querier interface {
	ClaimIdempotencyKey(ctx context.Context, key uuid.UUID) (int64, error)
	ConsumeIdempotencyKey(ctx context.Context, key uuid.UUID) (int64, error)
}

func (k Keys) Apply(ctx context.Context, q querier) error {
	if k.ClaimKey != uuid.Nil && k.ConsumeKey != uuid.Nil {
		return errors.New("both claim and consume idempotency keys provided")
	}

	switch {
	case k.ClaimKey != uuid.Nil:
		rows, err := q.ClaimIdempotencyKey(ctx, k.ClaimKey)
		if err != nil {
			return err
		}
		if rows == 0 {
			return sharedmodel.ErrDuplicateIdempotencyKey.Terminal()
		}
	case k.ConsumeKey != uuid.Nil:
		rows, err := q.ConsumeIdempotencyKey(ctx, k.ConsumeKey)
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
	}

	return nil
}
