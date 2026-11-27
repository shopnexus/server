package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// ReputationScreen is the ReputationScreen schema in
// api/schemas/screen_account.yaml.
type ReputationScreen struct {
	Rating ReputationRating `json:"rating"`
	// BuyerRating is the caller's rating as a buyer.
	BuyerRating ReputationRating   `json:"buyer_rating"`
	Sold        int                `json:"sold"`
	JoinedAt    time.Time          `json:"joined_at"`
	Followers   int                `json:"followers"`
	Reply       *chatsvc.ReplyRate `json:"reply"`
	Figures90d  BuyerFigures       `json:"figures_90d"`
	Caps        ReputationCaps     `json:"caps"`
}

// ReputationRating is the seller page's rating with a null average
// without reviews.
type ReputationRating struct {
	Average *float64 `json:"average"`
	Count   int      `json:"count"`
}

// BuyerFigures are the caller's COD orders over deal's
// dealdomain.BuyerPeriod and the failed ones among them, but parcels the
// carrier lost.
type BuyerFigures struct {
	CodOrders int `json:"cod_orders"`
	Failed    int `json:"failed"`
}

// ReputationCaps are the caps every account has and the caller's use of
// them.
type ReputationCaps struct {
	Listings  Cap `json:"listings"`
	CodOrders Cap `json:"cod_orders"`
}

// Cap is one cap and how much of it is used.
type Cap struct {
	Used int `json:"used"`
	Max  int `json:"max"`
}

// Reputation is UC-TRS-04. It reads the Uy tín của bạn view: the caller's
// ratings as a seller and as a buyer, sales, join date, followers and
// reply rate, its COD figures as a buyer, and the listing and COD caps
// with the caller's use. A deleted account is
// accountdomain.ErrSessionRevoked, from Me.
func (s *Screen) Reputation(ctx context.Context, account uuid.UUID) (ReputationScreen, error) {
	me, err := s.d.Accounts.Me(ctx, account)
	if err != nil {
		return ReputationScreen{}, fmt.Errorf("read the account: %w", err)
	}
	// 2. The figures; A1 when a role has no visible review.
	rating, err := s.d.Trust.Rating(ctx, account, trustdomain.RoleSeller)
	if err != nil {
		return ReputationScreen{}, fmt.Errorf("read the rating: %w", err)
	}
	buyerRating, err := s.d.Trust.Rating(ctx, account, trustdomain.RoleBuyer)
	if err != nil {
		return ReputationScreen{}, fmt.Errorf("read the buyer rating: %w", err)
	}
	counts, err := s.d.Catalog.SellerCounts(ctx, account)
	if err != nil {
		return ReputationScreen{}, fmt.Errorf("count the listings: %w", err)
	}
	followers, err := s.d.Accounts.FollowerCount(ctx, account)
	if err != nil {
		return ReputationScreen{}, fmt.Errorf("count the followers: %w", err)
	}
	// A2 · BR-TRS-05: chat leaves the rate out under 5 threads.
	reply, err := s.d.Chats.ReplyRate(ctx, account)
	if err != nil {
		return ReputationScreen{}, fmt.Errorf("read the reply rate: %w", err)
	}
	orders, err := s.d.Deals.BuyerOrders(ctx, account)
	if err != nil {
		return ReputationScreen{}, fmt.Errorf("count the orders: %w", err)
	}
	// 3. The caps every account has, with the caller's use.
	out := ReputationScreen{
		Rating: reputationRating(rating), BuyerRating: reputationRating(buyerRating), Sold: counts.Sold, JoinedAt: me.CreatedAt, Followers: followers,
		Reply: reply, Figures90d: BuyerFigures{CodOrders: orders.Placed, Failed: orders.Failed},
		Caps: ReputationCaps{
			Listings:  Cap{Used: counts.OnSale, Max: catalogdomain.MaxOnSale},
			CodOrders: Cap{Used: orders.Open, Max: dealdomain.MaxOpenOrders},
		},
	}
	return out, nil
}

// reputationRating is UC-TRS-04 A1: r with a null average without
// reviews.
func reputationRating(r trustsvc.Rating) ReputationRating {
	out := ReputationRating{Count: r.Count}
	if r.Count > 0 {
		out.Average = &r.Average
	}
	return out
}
