package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
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
// goes on; the read cap and a deleted account are refusals.
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

// openedRead is a Start's read after the steps every Start opens with:
// its ID and clock, or Answer, which Start returns as is.
type openedRead struct {
	ID     uuid.UUID
	At     time.Time
	Answer *StartResult
}

// openRead runs the steps every Start opens with: the clock, the account's
// status and phone, then newRead's insert under key. A refusal, or a read
// the key already started, is the Answer.
func (d Deps) openRead(ctx restate.Context, account, key uuid.UUID,
	newRead func(id uuid.UUID, at time.Time) domain.ListingRead,
) (openedRead, error) {
	at, err := restatex.Now(ctx, d.Now)
	if err != nil {
		return openedRead{}, err
	}
	status, err := restate.Run(ctx, func(rc restate.RunContext) (accountdomain.Status, error) {
		return d.Accounts.Status(rc, account)
	})
	if err != nil {
		return openedRead{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return openedRead{Answer: &StartResult{Refused: r}}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	// A read exists to post a listing, and posting needs a phone; asking
	// for it here keeps accounts made of an email alone off the model.
	hasPhone, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		return d.Accounts.HasPhone(rc, account)
	})
	if err != nil {
		return openedRead{}, fmt.Errorf("read the account's phone: %w", err)
	}
	if !hasPhone {
		return openedRead{Answer: &StartResult{Refused: accountdomain.ErrPhoneRequired}}, nil
	}
	id := restatex.NewID(ctx, at)
	ins, err := insertStep(ctx, d.Reads, newRead(id, at), key)
	if err != nil {
		return openedRead{}, err
	}
	if ins.Refused != nil {
		return openedRead{Answer: &StartResult{Refused: ins.Refused}}, nil //nolint:nilerr // A refusal goes back as data.
	}
	if ins.ID != id {
		return openedRead{Answer: &StartResult{ReadID: ins.ID}}, nil
	}
	return openedRead{ID: id, At: at}, nil
}

// sendRun sends a started read's delayed Expire, with expire as its input,
// and its Run.
func sendRun(ctx restate.Context, service string, id uuid.UUID, expire any) {
	restate.ServiceSend(ctx, service, "Expire").
		Send(expire, restate.WithDelay(domain.KeepFor), restate.WithIdempotencyKey(id.String()))
	restate.ServiceSend(ctx, service, "Run").Send(id, restate.WithIdempotencyKey(id.String()))
}

// deleteRead deletes a read in a step.
func (d Deps) deleteRead(ctx restate.Context, id uuid.UUID) error {
	return restate.RunVoid(ctx, func(rc restate.RunContext) error { return d.Reads.Delete(rc, id) }) //nolint:wrapcheck // The caller names the step.
}

// expireRead is Expire for a read whose source expires on its own.
func (d Deps) expireRead(ctx restate.Context, id uuid.UUID) error {
	if err := d.deleteRead(ctx, id); err != nil {
		return fmt.Errorf("delete read: %w", err)
	}
	return nil
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
