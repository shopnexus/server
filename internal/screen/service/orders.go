package service

import (
	"context"
	"fmt"
	"uuid"

	"golang.org/x/sync/errgroup"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/page"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// OrdersScreenItem is the OrdersScreenItem schema in
// api/schemas/screen_order.yaml.
type OrdersScreenItem struct {
	Order       dealsvc.OrderItem   `json:"order"`
	Counterpart chatsvc.PersonView  `json:"counterpart"`
	OwnReview   *trustsvc.OwnReview `json:"own_review,omitempty"`
}

// Orders reads a page of account's orders on side as, then the other
// parties and, for the buyer, account's reviews of the orders, one call
// each.
func (s *Screen) Orders(ctx context.Context, account uuid.UUID, as dealdomain.Party, p page.Request) (page.Page[OrdersScreenItem], error) {
	orders, err := s.deals.ListOrders(ctx, account, as, p)
	if err != nil {
		return page.Page[OrdersScreenItem]{}, fmt.Errorf("read orders: %w", err)
	}
	others, ids := make([]uuid.UUID, 0, len(orders.Items)), make([]uuid.UUID, 0, len(orders.Items))
	for i := range orders.Items {
		others, ids = append(others, orders.Items[i].CounterpartID), append(ids, orders.Items[i].ID)
	}
	// The people and the reviews depend only on the page; both reads run
	// at once.
	var (
		people  map[uuid.UUID]accountsvc.AccountView
		reviews map[uuid.UUID]trustsvc.OwnReview
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var peopleErr error
		if people, peopleErr = s.accounts.GetMany(gctx, others); peopleErr != nil {
			return fmt.Errorf("read people: %w", peopleErr)
		}
		return nil
	})
	if as == dealdomain.Buyer {
		g.Go(func() error {
			var reviewsErr error
			if reviews, reviewsErr = s.trust.OwnReviews(gctx, account, ids); reviewsErr != nil {
				return fmt.Errorf("read own reviews: %w", reviewsErr)
			}
			return nil
		})
	}
	if err = g.Wait(); err != nil {
		return page.Page[OrdersScreenItem]{}, err
	}
	items := make([]OrdersScreenItem, 0, len(orders.Items))
	for i := range orders.Items {
		o := orders.Items[i]
		item := OrdersScreenItem{Order: o, Counterpart: chatsvc.PersonFromAccount(o.CounterpartID, people[o.CounterpartID])}
		if as == dealdomain.Buyer {
			item.OwnReview = new(reviews[o.ID])
		}
		items = append(items, item)
	}
	return page.Page[OrdersScreenItem]{Items: items, NextCursor: orders.NextCursor}, nil
}
