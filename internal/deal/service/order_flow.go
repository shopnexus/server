package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/carrier"
)

// FlowInput names the order a flow moves, its deal, the caller and the
// caller's request key, if any. A shipment carries what the seller picked
// for it, and whether its pickup is the seller's saved one.
type FlowInput struct {
	SavedPickup bool                  `json:"saved_pickup,omitempty"`
	Caller      uuid.UUID             `json:"caller"`
	Key         uuid.UUID             `json:"key,omitzero"`
	DealID      uuid.UUID             `json:"deal_id"`
	OrderID     uuid.UUID             `json:"order_id"`
	Draft       *domain.ShipmentDraft `json:"draft,omitempty"`
}

// FlowResult carries a flow's refusal, or none.
type FlowResult struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r FlowResult) Refusal() *apperr.Error { return r.Refused }

// loaded is a deal as a flow's first step journals it: its state, the
// caller's side and its listing, or the refusal of a caller who is no side.
// Moved says the request's key is on a move of the caller's already.
type loaded struct {
	Deal    domain.DealState   `json:"deal"`
	By      domain.Party       `json:"by"`
	Listing catalogsvc.Summary `json:"listing"`
	Moved   bool               `json:"moved,omitempty"`
	Refused *apperr.Error      `json:"refused,omitempty"`
}

// loadStep reads the caller's deal and its listing in one journaled step,
// and whether the request with key moved the deal already (NFR-IDEM-01).
func loadStep(ctx restate.Context, s *Deals, caller, id, key uuid.UUID) (loaded, error) {
	out, err := restate.Run(ctx, func(rc restate.RunContext) (loaded, error) {
		d, by, stepErr := s.own(rc, caller, id)
		if errors.Is(stepErr, domain.ErrNotFound) {
			return loaded{Refused: domain.ErrNotFound}, nil
		}
		if stepErr != nil {
			return loaded{}, stepErr
		}
		if key != uuid.Nil() {
			moved, movedErr := s.d.Deals.Moved(rc, id, by, key)
			if movedErr != nil {
				return loaded{}, fmt.Errorf("find the key's move: %w", movedErr)
			}
			if moved {
				return loaded{Moved: true}, nil
			}
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

// saveStep saves d with its deal.moved event in one journaled step, under
// a write ID and an event ID minted from the journal, its moves keyed by
// the request's key or [uuid.Nil] for the system's. A deal changed
// meanwhile is domain.ErrChanged, a fourth open order domain.ErrCodLimit
// and a shipment at a shop the seller no longer links
// domain.ErrCarrierNotLinked, as data; a retry of a save that committed writes
// nothing more.
func saveStep(ctx restate.Context, deals repository.Deals, d *domain.Deal, key uuid.UUID, at time.Time) (*apperr.Error, error) {
	writeID, eventID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	out, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		stepErr := deals.Save(rc, d, writeID, key, events.Event{ID: eventID, Kind: events.DealMoved, SubjectID: d.ID(), OccurredAt: at})
		if errors.Is(stepErr, repository.ErrConflict) {
			return restatex.Refused{Refusal: domain.ErrChanged}, nil
		}
		// BR-DEAL-06: only a new pending order meets the cap.
		if errors.Is(stepErr, domain.ErrCodLimit) {
			return restatex.Refused{Refusal: domain.ErrCodLimit}, nil
		}
		// The seller unlinked the carrier, or linked another shop, since
		// the shipment was made.
		if errors.Is(stepErr, repository.ErrAccountChanged) {
			return restatex.Refused{Refusal: domain.ErrCarrierNotLinked}, nil
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

// keyMovedStep says, in one journaled step, whether a save refused with
// domain.ErrChanged lost to a racing try of the same keyed request.
func keyMovedStep(ctx restate.Context, deals repository.Deals, refused *apperr.Error, id uuid.UUID, by domain.Party, key uuid.UUID) (bool, error) {
	if refused == nil || refused.Code != domain.ErrChanged.Code || key == uuid.Nil() {
		return false, nil
	}
	moved, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		return deals.Moved(rc, id, by, key)
	})
	if err != nil {
		return false, fmt.Errorf("find the key's move: %w", err)
	}
	return moved, nil
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

// hideStep takes the listing held for the order off its hold as hidden by
// its seller in one journaled step: the carrier lost the parcel.
func hideStep(ctx restate.Context, catalog catalogsvc.Service, listing, order uuid.UUID) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return catalog.ReleaseHidden(rc, listing, order)
	}); err != nil {
		return fmt.Errorf("hide the listing: %w", err)
	}
	return nil
}

// sellStep marks the listing held for the order sold in one journaled step,
// under a write ID minted before it.
func sellStep(ctx restate.Context, catalog catalogsvc.Service, listing, order uuid.UUID, at time.Time) error {
	writeID := restatex.NewID(ctx, at)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return catalog.SellTo(rc, listing, order, writeID)
	}); err != nil {
		return fmt.Errorf("sell the listing: %w", err)
	}
	return nil
}

// callOffStep cancels a linked shipment at its carrier in one journaled
// step. A parcel the carrier lost leaves nothing to call off. A parcel it
// has picked up, or a refusal of shipping's such as a carrier to link
// again, may still ship: the step logs it for support and goes on, since
// a retry would not change the answer.
func callOffStep(ctx restate.Context, shipping Shipping, log *slog.Logger, seller uuid.UUID, sh domain.Shipment) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		callErr := shipping.Cancel(rc, seller, carrier.Carrier(sh.Carrier), sh.TrackingCode)
		switch {
		case callErr == nil, errors.Is(callErr, carrier.ErrNotFound):
			return nil
		case errors.Is(callErr, carrier.ErrNotCancellable), restatex.IsRefusal(callErr):
			log.WarnContext(rc, "shipment not called off; the parcel may still ship",
				"seller", seller, "carrier", sh.Carrier, "tracking_code", sh.TrackingCode, "err", callErr)
			return nil
		}
		return fmt.Errorf("cancel %s: %w", sh.TrackingCode, callErr)
	}); err != nil {
		return fmt.Errorf("call off the shipment at %s: %w", sh.Carrier, err)
	}
	return nil
}

// orderDeal is the deal of order. An unknown order and one the caller is
// neither side of are both domain.ErrOrderNotFound, so an order's existence
// does not leak.
func (s *Deals) orderDeal(ctx context.Context, caller, order uuid.UUID) (uuid.UUID, error) {
	r, err := s.d.Deals.Order(ctx, order)
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

// systemLoadStep reads a deal and its listing for a move of the system's
// in one journaled step.
func systemLoadStep(ctx restate.Context, s *Deals, id uuid.UUID) (loaded, error) {
	out, err := restate.Run(ctx, func(rc restate.RunContext) (loaded, error) {
		d, stepErr := s.d.Deals.Get(rc, id)
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
