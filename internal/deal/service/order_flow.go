package service

import (
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
)

// FlowInput names the deal a flow moves and the caller, with the waybill of
// a shipment or the outcome, delivered or returned, of a finish.
type FlowInput struct {
	Caller  uuid.UUID          `json:"caller"`
	DealID  uuid.UUID          `json:"deal_id"`
	Waybill *domain.Waybill    `json:"waybill,omitempty"`
	Outcome domain.OrderStatus `json:"outcome,omitempty"`
}

// FlowResult carries a flow's refusal, or none.
type FlowResult struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r FlowResult) Refusal() *apperr.Error { return r.Refused }

// WaybillInput is a waybill as the seller types it.
type WaybillInput struct {
	Carrier      string
	CarrierName  string
	TrackingCode string
	Fee          int64
}

// loaded is a deal as a flow's first step journals it: its state, the
// caller's side and its listing, or the refusal of a caller who is no side.
type loaded struct {
	Deal    domain.DealState   `json:"deal"`
	By      domain.Party       `json:"by"`
	Listing catalogsvc.Summary `json:"listing"`
	Refused *apperr.Error      `json:"refused,omitempty"`
}

// refusal is what a step decided: a refusal, or none.
type refusal struct {
	Refused *apperr.Error `json:"refused,omitempty"`
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
	out, err := restate.Run(ctx, func(rc restate.RunContext) (refusal, error) {
		stepErr := deals.Save(rc, d, writeID, events.Event{ID: eventID, Kind: events.DealMoved, SubjectID: d.ID(), OccurredAt: at})
		if errors.Is(stepErr, repository.ErrConflict) {
			return refusal{Refused: domain.ErrChanged}, nil
		}
		if stepErr != nil {
			return refusal{}, fmt.Errorf("save deal: %w", stepErr)
		}
		return refusal{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("save the deal: %w", err)
	}
	return out.Refused, nil
}

// reserveStep holds the listing for the order in one journaled step; a
// listing that is not on sale is domain.ErrListingUnavailable, as data.
func reserveStep(ctx restate.Context, catalog catalogsvc.Service, listing, order uuid.UUID) (*apperr.Error, error) {
	out, err := restate.Run(ctx, func(rc restate.RunContext) (refusal, error) {
		stepErr := catalog.Reserve(rc, listing, order)
		if errors.Is(stepErr, catalogdomain.ErrStateConflict) {
			return refusal{Refused: domain.ErrListingUnavailable}, nil
		}
		if stepErr != nil {
			return refusal{}, fmt.Errorf("reserve: %w", stepErr)
		}
		return refusal{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("reserve the listing: %w", err)
	}
	return out.Refused, nil
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
