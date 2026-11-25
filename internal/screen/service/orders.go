package service

import (
	"context"
	"fmt"
	"uuid"

	"golang.org/x/sync/errgroup"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/page"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// OrdersScreen is the OrdersScreen schema in api/schemas/screen_order.yaml:
// a page of rows and the side's count, CodOrders for the buyer and
// AwaitingShipment for the seller.
type OrdersScreen struct {
	Items            []OrdersScreenItem `json:"items"`
	NextCursor       *uuid.UUID         `json:"next_cursor,omitempty"`
	CodOrders        *Cap               `json:"cod_orders,omitempty"`
	AwaitingShipment *int               `json:"awaiting_shipment,omitempty"`
}

// OrdersScreenItem is the OrdersScreenItem schema in
// api/schemas/screen_order.yaml.
type OrdersScreenItem struct {
	Order       dealsvc.OrderItem   `json:"order"`
	Counterpart chatsvc.PersonView  `json:"counterpart"`
	OwnReview   *trustsvc.OwnReview `json:"own_review,omitempty"`
}

// Orders reads a page of account's orders on side as, then the other
// parties, account's reviews of the orders and the side's count, one call
// each.
func (s *Screen) Orders(ctx context.Context, account uuid.UUID, as dealdomain.Party, p page.Request) (OrdersScreen, error) {
	orders, err := s.d.Deals.ListOrders(ctx, account, as, p)
	if err != nil {
		return OrdersScreen{}, fmt.Errorf("read orders: %w", err)
	}
	others, ids := make([]uuid.UUID, 0, len(orders.Items)), make([]uuid.UUID, 0, len(orders.Items))
	for i := range orders.Items {
		others, ids = append(others, orders.Items[i].CounterpartID), append(ids, orders.Items[i].ID)
	}
	// The people and the reviews depend only on the page, the count on the
	// side; the reads run at once.
	var (
		people  map[uuid.UUID]accountsvc.AccountView
		reviews map[uuid.UUID]trustsvc.OrderReviews
		out     OrdersScreen
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if as == dealdomain.Seller {
			n, countErr := s.d.Deals.AwaitingShipment(gctx, account)
			if countErr != nil {
				return fmt.Errorf("count pending sales: %w", countErr)
			}
			out.AwaitingShipment = &n
			return nil
		}
		n, countErr := s.d.Deals.BuyerOrders(gctx, account)
		if countErr != nil {
			return fmt.Errorf("count COD orders: %w", countErr)
		}
		out.CodOrders = &Cap{Used: n.Open, Max: dealdomain.MaxOpenOrders}
		return nil
	})
	g.Go(func() error {
		var peopleErr error
		if people, peopleErr = s.d.Accounts.GetMany(gctx, others); peopleErr != nil {
			return fmt.Errorf("read people: %w", peopleErr)
		}
		return nil
	})
	g.Go(func() error {
		var reviewsErr error
		if reviews, reviewsErr = s.d.Trust.OrderReviews(gctx, account, ids); reviewsErr != nil {
			return fmt.Errorf("read own reviews: %w", reviewsErr)
		}
		return nil
	})
	if err = g.Wait(); err != nil {
		return OrdersScreen{}, err
	}
	out.Items, out.NextCursor = make([]OrdersScreenItem, 0, len(orders.Items)), orders.NextCursor
	for i := range orders.Items {
		o := orders.Items[i]
		item := OrdersScreenItem{Order: o, Counterpart: chatsvc.PersonFromAccount(o.CounterpartID, people[o.CounterpartID])}
		item.OwnReview = new(reviews[o.ID].Own)
		out.Items = append(out.Items, item)
	}
	return out, nil
}
