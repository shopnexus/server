package service

import (
	"context"
	"fmt"
	"uuid"
)

// Seller reads the account's public fields, the average and count over
// every review of the seller, and how many listings the seller sold and
// has on sale. An unknown account is accountdomain.ErrAccountNotFound.
func (s *Trust) Seller(ctx context.Context, id uuid.UUID) (SellerView, error) {
	a, err := s.accounts.Get(ctx, id)
	if err != nil {
		return SellerView{}, fmt.Errorf("read the seller: %w", err)
	}
	average, count, err := s.reviews.Summary(ctx, id)
	if err != nil {
		return SellerView{}, fmt.Errorf("summarize reviews: %w", err)
	}
	n, err := s.catalog.SellerCounts(ctx, id)
	if err != nil {
		return SellerView{}, fmt.Errorf("count listings: %w", err)
	}
	return SellerView{
		ID: id, DisplayName: a.DisplayName, AvatarURL: a.AvatarURL, Bio: a.Bio, JoinedAt: a.CreatedAt,
		Rating: Rating{Average: average, Count: count}, Sold: n.Sold, OnSale: n.OnSale,
	}, nil
}
