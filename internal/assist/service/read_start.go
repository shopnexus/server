package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/assist/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// insertedRead is the insert step's outcome: the read that holds the
// request's key, or the refusal.
type insertedRead struct {
	ID      uuid.UUID     `json:"id"`
	Refused *apperr.Error `json:"refused,omitempty"`
}

// insertStep inserts read under the account's request key in one journaled
// step. The ID is read's, or a racing retry's of the same key, whose run
// goes on; the read cap and a deleted account are refusals. Each source's
// Start calls it.
func insertStep(ctx restate.Context, reads repository.ListingReads, read domain.ListingRead, key uuid.UUID) (insertedRead, error) {
	out, err := restate.Run(ctx, func(rc restate.RunContext) (insertedRead, error) {
		id, stepErr := reads.Insert(rc, read, key)
		if stepErr != nil {
			refused, faultErr := restatex.AsRefusal(stepErr)
			if faultErr != nil {
				return insertedRead{}, fmt.Errorf("insert read: %w", faultErr)
			}
			return insertedRead{Refused: refused.Refusal}, nil
		}
		return insertedRead{ID: id}, nil
	})
	if err != nil {
		return insertedRead{}, fmt.Errorf("insert read: %w", err)
	}
	return out, nil
}

// readOfKey is the read account started with key, as GetRead returns it,
// and true; a retry's answer (NFR-IDEM-01). Each source's Start* calls it.
func (a *Assist) readOfKey(ctx context.Context, account, key uuid.UUID) (ListingReadView, bool, error) {
	id, err := a.d.Reads.ByKey(ctx, account, key)
	if errors.Is(err, domain.ErrNotFound) {
		return ListingReadView{}, false, nil
	}
	if err != nil {
		return ListingReadView{}, false, fmt.Errorf("find the key's read: %w", err)
	}
	v, err := a.GetRead(ctx, account, id)
	return v, err == nil, err
}
