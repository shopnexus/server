package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// reviewable are the order statuses a first review may be given in: the
// order closed with a delivery or a failed one.
var reviewable = []dealdomain.OrderStatus{dealdomain.OrderCompleted, dealdomain.OrderFailed}

// PutReview checks that the caller may write and is a party to the
// order, then creates their review of the other party, once the order is
// delivered or failed, or edits it within 7 days and before a reply.
func (s *Trust) PutReview(ctx context.Context, caller, order uuid.UUID, stars int, text string) (ReviewView, error) {
	status, err := s.accounts.Status(ctx, caller)
	if err != nil {
		return ReviewView{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return ReviewView{}, r
	}
	o, role, subject, err := s.partyOrder(ctx, caller, order)
	if err != nil {
		return ReviewView{}, err
	}
	now := s.now()
	r, err := s.reviews.ByOrder(ctx, order, role)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		if !slices.Contains(reviewable, o.Status) || o.ClosedAt == nil {
			return ReviewView{}, domain.ErrNotDelivered
		}
		_, otherErr := s.reviews.ByOrder(ctx, order, role.Other())
		if otherErr != nil && !errors.Is(otherErr, repository.ErrNotFound) {
			return ReviewView{}, fmt.Errorf("get the other review: %w", otherErr)
		}
		visibleAt := domain.RevealAt(*o.ClosedAt, otherErr == nil, now)
		if r, err = domain.NewReview(uuid.NewV7(), order, role, caller, subject, o.ListingID, stars, text, visibleAt, now); err != nil {
			return ReviewView{}, err
		}
		err = s.reviews.Insert(ctx, r)
	case err != nil:
		return ReviewView{}, fmt.Errorf("get review: %w", err)
	default:
		if err = r.Edit(stars, text, now); err != nil {
			return ReviewView{}, err
		}
		err = s.reviews.Save(ctx, &r, uuid.NewV7())
	}
	if errors.Is(err, repository.ErrConflict) {
		return ReviewView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return ReviewView{}, fmt.Errorf("save review: %w", err)
	}
	views, err := s.views(ctx, []domain.Review{r})
	if err != nil {
		return ReviewView{}, err
	}
	return views[0], nil
}

// DeleteReview takes the caller's review of an order down within 7 days of
// its first submission and before a reply; a review posted again later
// keeps that start.
func (s *Trust) DeleteReview(ctx context.Context, caller, order uuid.UUID) error {
	_, role, _, err := s.partyOrder(ctx, caller, order)
	if err != nil {
		return err
	}
	r, err := s.reviews.ByOrder(ctx, order, role)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && r.Deleted()) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get review: %w", err)
	}
	if err = r.Delete(s.now()); err != nil {
		return err
	}
	err = s.reviews.Save(ctx, &r, uuid.NewV7())
	if errors.Is(err, repository.ErrConflict) {
		return apperr.ErrTryAgain
	}
	if err != nil {
		return fmt.Errorf("delete review: %w", err)
	}
	return nil
}

// ReplyReview writes the subject's one reply to a visible review, which
// closes it. Anyone but the subject gets review_not_found.
func (s *Trust) ReplyReview(ctx context.Context, caller, id uuid.UUID, text string) (ReviewView, error) {
	status, err := s.accounts.Status(ctx, caller)
	if err != nil {
		return ReviewView{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return ReviewView{}, r
	}
	r, err := s.reviews.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return ReviewView{}, domain.ErrNotFound
	}
	if err != nil {
		return ReviewView{}, fmt.Errorf("get review: %w", err)
	}
	if err = r.Reply(caller, text, s.now()); err != nil {
		return ReviewView{}, err
	}
	err = s.reviews.Save(ctx, &r, uuid.NewV7())
	if errors.Is(err, repository.ErrConflict) {
		return ReviewView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return ReviewView{}, fmt.Errorf("save reply: %w", err)
	}
	views, err := s.views(ctx, []domain.Review{r})
	if err != nil {
		return ReviewView{}, err
	}
	return views[0], nil
}

// Reviews reads one page of the visible reviews of subject in role,
// newest first. An unknown account is accountdomain.ErrAccountNotFound.
func (s *Trust) Reviews(ctx context.Context, subject uuid.UUID, role domain.Role, p page.Request) (page.Page[ReviewView], error) {
	if _, err := s.accounts.Get(ctx, subject); err != nil {
		return page.Page[ReviewView]{}, fmt.Errorf("read the account: %w", err)
	}
	rows, err := s.reviews.OfSubject(ctx, subject, role, s.now(), p)
	if err != nil {
		return page.Page[ReviewView]{}, fmt.Errorf("list reviews: %w", err)
	}
	views, err := s.views(ctx, rows)
	if err != nil {
		return page.Page[ReviewView]{}, err
	}
	return page.Of(views, p, func(v ReviewView) uuid.UUID { return v.ID }), nil
}

// partyOrder reads an order the caller is a party to, with the role of the
// review the caller writes and its subject; any other order is
// domain.ErrOrderNotFound, so an order's existence does not leak.
func (s *Trust) partyOrder(ctx context.Context, caller, order uuid.UUID) (dealsvc.OrderFacts, domain.Role, uuid.UUID, error) {
	o, found, err := s.deals.Order(ctx, order)
	if err != nil {
		return dealsvc.OrderFacts{}, "", uuid.UUID{}, fmt.Errorf("get order: %w", err)
	}
	switch {
	case !found:
		return dealsvc.OrderFacts{}, "", uuid.UUID{}, domain.ErrOrderNotFound
	case o.BuyerID == caller:
		return o, domain.RoleSeller, o.SellerID, nil
	case o.SellerID == caller:
		return o, domain.RoleBuyer, o.BuyerID, nil
	default:
		return dealsvc.OrderFacts{}, "", uuid.UUID{}, domain.ErrOrderNotFound
	}
}
