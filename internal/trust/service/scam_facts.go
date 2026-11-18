package service

import (
	"context"
	"fmt"
	"slices"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/trust/domain"
)

// AccountAge tells new_account for a suspect whose account is under a week
// old.
type AccountAge struct {
	Accounts accountsvc.Service
}

func (f AccountAge) Signals(ctx context.Context, suspect uuid.UUID, _ domain.Window, now time.Time) ([]domain.Signal, error) {
	a, err := f.Accounts.Get(ctx, suspect)
	if err != nil {
		return nil, fmt.Errorf("read the suspect's account: %w", err)
	}
	if s, ok := domain.NewAccountSignal(a.CreatedAt, now); ok {
		return []domain.Signal{s}, nil
	}
	return nil, nil
}

// MarketGap tells price_far_below when the newest listing of the suspect's
// that the window names is priced far below listings like it. TextMin and
// PhotoMin are search's cosine floors.
type MarketGap struct {
	Catalog           catalogsvc.Service
	TextMin, PhotoMin float64
}

func (f MarketGap) Signals(ctx context.Context, suspect uuid.UUID, w domain.Window, _ time.Time) ([]domain.Signal, error) {
	asked := map[uuid.UUID]bool{}
	for _, l := range slices.Backward(w.Lines) {
		id := l.ListingID
		if id == nil || asked[*id] {
			continue
		}
		asked[*id] = true
		m, ok, err := f.Catalog.MarketPrice(ctx, *id, f.TextMin, f.PhotoMin)
		if err != nil {
			return nil, fmt.Errorf("read the market price: %w", err)
		}
		if !ok || m.SellerID != suspect {
			continue
		}
		if s, holds := domain.PriceBelowSignal(m.Price, m.Median, m.SampleSize); holds {
			return []domain.Signal{s}, nil
		}
		return nil, nil
	}
	return nil, nil
}
