package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// reviewable are the order statuses a first review may be given in: the
// order closed with a delivery or a failed one. A parcel the carrier lost
// is neither side's doing, so its order takes no review.
var reviewable = []dealdomain.OrderStatus{dealdomain.OrderCompleted, dealdomain.OrderFailed}

// PutReview is UC-TRS-01 steps 1 to 3 and A1's edit. It checks that the
// caller may write and is a party to the order, then creates their review
// of the other party, once the order is delivered or failed, or edits it
// while it is hidden, within 7 days and before a reply. Its new photos
// are attached to the order in media and processed first; nil tags or
// photos keep the review's. A review that already holds the request, as a
// retry or a double tap finds it, is answered as it is.
func (s *Trust) PutReview(ctx context.Context, caller, order uuid.UUID, stars int, text string, tags []domain.Tag, photos []uuid.UUID) (ReviewView, error) {
	// E1 · BR-MOD-02
	status, err := s.d.Accounts.Status(ctx, caller)
	if err != nil {
		return ReviewView{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return ReviewView{}, r
	}
	// 2. The caller is a party to the order; E4 otherwise.
	o, role, subject, err := s.partyOrder(ctx, caller, order)
	if err != nil {
		return ReviewView{}, err
	}
	now := s.d.Now()
	r, err := s.d.Reviews.ByOrder(ctx, order, role)
	found := err == nil
	switch {
	case errors.Is(err, repository.ErrNotFound):
		// E5: also a parcel the carrier lost.
		lost := o.Shipment != nil && o.Shipment.Status == dealdomain.ShipmentLost
		if !slices.Contains(reviewable, o.Status) || o.ClosedAt == nil || lost {
			return ReviewView{}, domain.ErrNotDelivered
		}
	case err != nil:
		return ReviewView{}, fmt.Errorf("get review: %w", err)
	case r.Holds(stars, text, tags, photos):
		// A2 · NFR-IDEM-01
		return s.reviewView(ctx, caller, r)
	default:
		// A1: an edit while hidden; E6, E7 otherwise.
		if err = r.CanChange(now); err != nil {
			return ReviewView{}, err
		}
	}
	// The order holds the photos: a first review's ID is new on each try,
	// and a retry must find its photos still free for it.
	kept := r.Photos()
	if photos != nil {
		known := make(map[uuid.UUID]domain.Photo, len(kept))
		for _, p := range kept {
			known[p.UploadID] = p
		}
		fresh := make([]uuid.UUID, 0, len(photos))
		for _, id := range photos {
			if _, ok := known[id]; !ok {
				fresh = append(fresh, id)
			}
		}
		var processed map[uuid.UUID]mediasvc.ProcessResult
		// E3: media refuses an upload that is not the caller's.
		if len(fresh) > 0 {
			if processed, err = s.d.Media.Photos(ctx, caller, order, fresh); err != nil {
				return ReviewView{}, fmt.Errorf("process the photos: %w", err)
			}
		}
		kept = make([]domain.Photo, 0, len(photos))
		for _, id := range photos {
			p, ok := known[id]
			if !ok {
				p = domain.Photo{UploadID: id, PhotoKey: processed[id].PhotoKey, ThumbKey: processed[id].ThumbKey}
			}
			kept = append(kept, p)
		}
	}
	if tags == nil {
		tags = r.Tags()
	}
	// 3. Save the review, hidden until the other side reviews or the window
	// ends; E2 when it breaks a rule.
	if !found {
		_, otherErr := s.d.Reviews.ByOrder(ctx, order, role.Other())
		if otherErr != nil && !errors.Is(otherErr, repository.ErrNotFound) {
			return ReviewView{}, fmt.Errorf("get the other review: %w", otherErr)
		}
		visibleAt := domain.RevealAt(*o.ClosedAt, otherErr == nil, now)
		if r, err = domain.NewReview(uuid.NewV7(), order, role, caller, subject, o.ListingID, stars, text, tags, kept, visibleAt, now); err != nil {
			return ReviewView{}, err
		}
		err = s.d.Reviews.Insert(ctx, r)
	} else {
		if err = r.Edit(stars, text, tags, kept, now); err != nil {
			return ReviewView{}, err
		}
		err = s.d.Reviews.Save(ctx, &r, uuid.NewV7())
	}
	// E9 · NFR-CONC-01
	if errors.Is(err, repository.ErrConflict) {
		// A double tap's twin may have written the same review.
		if again, getErr := s.d.Reviews.ByOrder(ctx, order, role); getErr == nil && again.Holds(stars, text, tags, photos) {
			return s.reviewView(ctx, caller, again)
		}
		return ReviewView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return ReviewView{}, fmt.Errorf("save review: %w", err)
	}
	return s.reviewView(ctx, caller, r)
}

// reviewView is r as the caller reads it.
func (s *Trust) reviewView(ctx context.Context, caller uuid.UUID, r domain.Review) (ReviewView, error) {
	views, err := s.views(ctx, &caller, []domain.Review{r})
	if err != nil {
		return ReviewView{}, err
	}
	return views[0], nil
}

// DeleteReview is UC-TRS-01 A1's delete. It takes the caller's review of
// an order down while it is hidden, within 7 days of its first submission
// and before a reply; a review posted again later keeps that start. The
// caller's review deleted already is done (a retry).
func (s *Trust) DeleteReview(ctx context.Context, caller, order uuid.UUID) error {
	// E1 · BR-MOD-02
	status, err := s.d.Accounts.Status(ctx, caller)
	if err != nil {
		return fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return r
	}
	// E4
	_, role, _, err := s.partyOrder(ctx, caller, order)
	if err != nil {
		return err
	}
	r, err := s.d.Reviews.ByOrder(ctx, order, role)
	// E8
	if errors.Is(err, repository.ErrNotFound) || (err == nil && r.ReviewerID() != caller) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get review: %w", err)
	}
	// A2 · NFR-IDEM-01
	if r.Deleted() {
		return nil
	}
	// A1: a delete while hidden; E6, E7 otherwise.
	if err = r.Delete(s.d.Now()); err != nil {
		return err
	}
	err = s.d.Reviews.Save(ctx, &r, uuid.NewV7())
	// E9 · NFR-CONC-01
	if errors.Is(err, repository.ErrConflict) {
		if again, getErr := s.d.Reviews.ByOrder(ctx, order, role); getErr == nil && again.Deleted() {
			return nil
		}
		return apperr.ErrTryAgain
	}
	if err != nil {
		return fmt.Errorf("delete review: %w", err)
	}
	return nil
}

// ReplyReview is UC-TRS-01 step 4. It writes the subject's one reply to a
// visible review, which closes it. Anyone but the subject gets
// review_not_found. The same reply there already is answered as it is (a
// retry).
func (s *Trust) ReplyReview(ctx context.Context, caller, id uuid.UUID, text string) (ReviewView, error) {
	// E1 · BR-MOD-02
	status, err := s.d.Accounts.Status(ctx, caller)
	if err != nil {
		return ReviewView{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return ReviewView{}, r
	}
	r, err := s.d.Reviews.Get(ctx, id)
	// E12
	if errors.Is(err, repository.ErrNotFound) {
		return ReviewView{}, domain.ErrNotFound
	}
	if err != nil {
		return ReviewView{}, fmt.Errorf("get review: %w", err)
	}
	// 4. The subject's one reply; E2, E10, E11, E12 from the review.
	if err = r.Reply(caller, text, s.d.Now()); err != nil {
		// A2 · NFR-IDEM-01
		if errors.Is(err, domain.ErrReplied) && r.HasReply(text) {
			return s.reviewView(ctx, caller, r)
		}
		return ReviewView{}, err
	}
	err = s.d.Reviews.Save(ctx, &r, uuid.NewV7())
	// E9 · NFR-CONC-01
	if errors.Is(err, repository.ErrConflict) {
		if again, getErr := s.d.Reviews.Get(ctx, id); getErr == nil && again.SubjectID() == caller && again.HasReply(text) {
			return s.reviewView(ctx, caller, again)
		}
		return ReviewView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return ReviewView{}, fmt.Errorf("save reply: %w", err)
	}
	return s.reviewView(ctx, caller, r)
}

// Reviews reads one page of the visible reviews of subject in role,
// newest first, with viewer's helpful marks. An unknown account is
// accountdomain.ErrAccountNotFound.
func (s *Trust) Reviews(ctx context.Context, viewer *uuid.UUID, subject uuid.UUID, role domain.Role, p page.Request) (page.Page[ReviewView], error) {
	if _, err := s.d.Accounts.Get(ctx, subject); err != nil {
		return page.Page[ReviewView]{}, fmt.Errorf("read the account: %w", err)
	}
	rows, err := s.d.Reviews.OfSubject(ctx, subject, role, s.d.Now(), p)
	if err != nil {
		return page.Page[ReviewView]{}, fmt.Errorf("list reviews: %w", err)
	}
	views, err := s.views(ctx, viewer, rows)
	if err != nil {
		return page.Page[ReviewView]{}, err
	}
	return page.Of(views, p, func(v ReviewView) uuid.UUID { return v.ID }), nil
}

// partyOrder reads an order the caller is a party to, with the role of the
// review the caller writes and its subject; any other order is
// domain.ErrOrderNotFound, so an order's existence does not leak.
func (s *Trust) partyOrder(ctx context.Context, caller, order uuid.UUID) (dealsvc.OrderFacts, domain.Role, uuid.UUID, error) {
	o, found, err := s.d.Deals.Order(ctx, order)
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
