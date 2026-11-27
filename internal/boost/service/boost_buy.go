package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/boost/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	paymentdomain "github.com/shopnexus/server/internal/payment/domain"
	paymentsvc "github.com/shopnexus/server/internal/payment/service"
)

// Buy is UC-LST-03, steps 2–6: the checks in the order the spec lists
// them, then a payment of the package's price and the boost awaiting it.
// The store checks the listing's live boost again under its lock.
func (s *Boosts) Buy(ctx context.Context, in BuyInput) (View, bool, error) {
	// E1
	if !s.d.Payments.Available() {
		return View{}, false, paymentdomain.ErrUnavailable
	}
	// E2
	pkg, ok := s.pkg(in.Package)
	if !ok {
		return View{}, false, domain.ErrUnknownPackage
	}
	// A1
	prior, err := s.d.Store.ByKey(ctx, in.Seller, in.Key)
	switch {
	case err == nil:
		v, viewErr := s.one(ctx, prior)
		return v, false, viewErr
	case !errors.Is(err, domain.ErrNotFound):
		return View{}, false, fmt.Errorf("read the purchase of the key: %w", err)
	default:
	}
	// 3.
	refused, err := s.sellerRefusal(ctx, in.Seller)
	if err != nil {
		return View{}, false, err
	}
	if refused != nil {
		return View{}, false, refused
	}
	// 4.
	l, found, err := s.d.Catalog.Summary(ctx, in.ListingID)
	if err != nil {
		return View{}, false, fmt.Errorf("read the listing: %w", err)
	}
	if !found || l.SellerID != in.Seller || l.Status == catalogdomain.StatusDeleted {
		return View{}, false, catalogdomain.ErrNotFound // E5
	}
	if !onSale(l.Status) {
		return View{}, false, domain.ErrNotBoostable // E6
	}
	// 5.
	live, err := s.d.Store.Live(ctx, []uuid.UUID{in.ListingID})
	if err != nil {
		return View{}, false, fmt.Errorf("read the listing's boost: %w", err)
	}
	if b, ok := live[in.ListingID]; ok {
		switch {
		case b.Status() == domain.StatusActive:
			return View{}, false, domain.ErrActive.WithBoost(b.ID()) //nolint:wrapcheck // E7, a refusal as it is
		case b.Package() == pkg.ID:
			v, viewErr := s.one(ctx, b) // A2
			return v, false, viewErr
		default:
			return View{}, false, domain.ErrPending.WithBoost(b.ID()) //nolint:wrapcheck // E10, a refusal as it is
		}
	}
	// 6.
	id := uuid.NewV7()
	p, err := s.d.Payments.Open(ctx, paymentsvc.OpenInput{
		Account: in.Seller, Product: paymentdomain.ProductBoost, ProductID: id, Amount: pkg.Price, IP: in.IP, English: in.English,
	})
	if err != nil {
		return View{}, false, fmt.Errorf("open the boost's payment: %w", err)
	}
	b, existed, err := s.d.Store.Insert(ctx, domain.NewBoost(id, in.ListingID, in.Seller, in.Key, pkg, p.ID, p.CreatedAt))
	if err != nil {
		return View{}, false, fmt.Errorf("store the boost: %w", err)
	}
	if existed {
		v, viewErr := s.one(ctx, b)
		return v, false, viewErr
	}
	return view(b, &p), true, nil
}

// sellerRefusal is UC-LST-03, step 3: why the seller may buy no package,
// or nil. err is a failed read.
func (s *Boosts) sellerRefusal(ctx context.Context, seller uuid.UUID) (refused, err error) {
	status, err := s.d.Accounts.Status(ctx, seller)
	if err != nil {
		return nil, fmt.Errorf("read the seller's status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return r, nil // E3
	}
	hasPhone, err := s.d.Accounts.HasPhone(ctx, seller)
	if err != nil {
		return nil, fmt.Errorf("read the seller's phone: %w", err)
	}
	if !hasPhone {
		return accountdomain.ErrPhoneRequired, nil // E4
	}
	return nil, nil
}

// onSale is a listing status a package may be bought for (BR-LST-05).
func onSale(st catalogdomain.Status) bool { return st == catalogdomain.StatusActive }
