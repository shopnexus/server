package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// FlowInput names the order a flow moves, its deal and the caller. A
// shipment carries either its manual shipment, read from the carrier, or
// what the seller picked for a linked one.
type FlowInput struct {
	SavedPickup bool                `json:"saved_pickup,omitempty"`
	Caller      uuid.UUID           `json:"caller"`
	DealID      uuid.UUID           `json:"deal_id"`
	OrderID     uuid.UUID           `json:"order_id"`
	Manual      *domain.Shipment    `json:"manual,omitempty"`
	Linked      *domain.LinkedDraft `json:"linked,omitempty"`
}

// FlowResult carries a flow's refusal, or none.
type FlowResult struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r FlowResult) Refusal() *apperr.Error { return r.Refused }

// loaded is a deal as a flow's first step journals it: its state, the
// caller's side and its listing, or the refusal of a caller who is no side.
type loaded struct {
	Deal    domain.DealState   `json:"deal"`
	By      domain.Party       `json:"by"`
	Listing catalogsvc.Summary `json:"listing"`
	Refused *apperr.Error      `json:"refused,omitempty"`
}

// loadStep reads the caller's deal and its listing in one journaled step.
func loadStep(ctx restate.Context, s *Deals, caller, id uuid.UUID) (loaded, error) {
	out, err := restate.Run(ctx, func(rc restate.RunContext) (loaded, error) {
		d, by, stepErr := s.own(rc, caller, id)
		if errors.Is(stepErr, domain.ErrNotFound) {
			return loaded{Refused: domain.ErrNotFound}, nil
		}
		if stepErr != nil {
			return loaded{}, stepErr
		}
		l, stepErr := s.listing(rc, d.ListingID())
		if stepErr != nil {
			return loaded{}, stepErr
		}
		return loaded{Deal: d.State(), By: by, Listing: l}, nil
	})
	if err != nil {
		return loaded{}, fmt.Errorf("load the deal: %w", err)
	}
	return out, nil
}

// saveStep saves d under writeID with deal.moved as eventID in one
// journaled step. A deal changed meanwhile is domain.ErrChanged, as data;
// a retry of a save that committed writes nothing more.
func saveStep(ctx restate.Context, deals repository.Deals, d *domain.Deal, writeID, eventID uuid.UUID, at time.Time) (*apperr.Error, error) {
	out, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		stepErr := deals.Save(rc, d, writeID, events.Event{ID: eventID, Kind: events.DealMoved, SubjectID: d.ID(), OccurredAt: at})
		if errors.Is(stepErr, repository.ErrConflict) {
			return restatex.Refused{Refusal: domain.ErrChanged}, nil
		}
		if stepErr != nil {
			return restatex.Refused{}, fmt.Errorf("save deal: %w", stepErr)
		}
		return restatex.Refused{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("save the deal: %w", err)
	}
	return out.Refusal, nil
}

// reserveStep holds the listing for the order in one journaled step; a
// listing that is not on sale is domain.ErrListingUnavailable, as data.
func reserveStep(ctx restate.Context, catalog catalogsvc.Service, listing, order uuid.UUID) (*apperr.Error, error) {
	out, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		stepErr := catalog.Reserve(rc, listing, order)
		if errors.Is(stepErr, catalogdomain.ErrStateConflict) {
			return restatex.Refused{Refusal: domain.ErrListingUnavailable}, nil
		}
		if stepErr != nil {
			return restatex.Refused{}, fmt.Errorf("reserve: %w", stepErr)
		}
		return restatex.Refused{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("reserve the listing: %w", err)
	}
	return out.Refusal, nil
}

// releaseStep puts the listing held for the order back on sale in one
// journaled step.
func releaseStep(ctx restate.Context, catalog catalogsvc.Service, listing, order uuid.UUID) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return catalog.Release(rc, listing, order)
	}); err != nil {
		return fmt.Errorf("release the listing: %w", err)
	}
	return nil
}

// sellStep marks the listing held for the order sold in one journaled step.
func sellStep(ctx restate.Context, catalog catalogsvc.Service, listing, order uuid.UUID) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return catalog.SellTo(rc, listing, order)
	}); err != nil {
		return fmt.Errorf("sell the listing: %w", err)
	}
	return nil
}

// orderDeal is the deal of order. An unknown order and one the caller is
// neither side of are both domain.ErrOrderNotFound, so an order's existence
// does not leak.
func (s *Deals) orderDeal(ctx context.Context, caller, order uuid.UUID) (uuid.UUID, error) {
	r, err := s.deals.Order(ctx, order)
	if errors.Is(err, repository.ErrNotFound) {
		return uuid.Nil(), domain.ErrOrderNotFound
	}
	if err != nil {
		return uuid.Nil(), fmt.Errorf("get order: %w", err)
	}
	if caller != r.BuyerID && caller != r.SellerID {
		return uuid.Nil(), domain.ErrOrderNotFound
	}
	return r.DealID, nil
}

// checkSeller tells whether the caller is the order's seller: someone
// else's order is domain.ErrOrderNotFound, the caller's purchase
// domain.ErrMoveNotAllowed.
func (s *Deals) checkSeller(ctx context.Context, caller, order uuid.UUID) error {
	r, err := s.deals.Order(ctx, order)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrOrderNotFound
	}
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	switch caller {
	case r.SellerID:
		return nil
	case r.BuyerID:
		return domain.ErrMoveNotAllowed
	}
	return domain.ErrOrderNotFound
}

// systemLoadStep reads a deal and its listing for a move of the system's
// in one journaled step.
func systemLoadStep(ctx restate.Context, s *Deals, id uuid.UUID) (loaded, error) {
	out, err := restate.Run(ctx, func(rc restate.RunContext) (loaded, error) {
		d, stepErr := s.deals.Get(rc, id)
		if stepErr != nil {
			return loaded{}, fmt.Errorf("get deal: %w", stepErr)
		}
		l, stepErr := s.listing(rc, d.ListingID())
		if stepErr != nil {
			return loaded{}, stepErr
		}
		return loaded{Deal: d.State(), By: domain.System, Listing: l}, nil
	})
	if err != nil {
		return loaded{}, fmt.Errorf("load the deal: %w", err)
	}
	return out, nil
}
