package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// Follow is UC-DSC-04 A2. It checks that the follower may write and the
// account exists and is not deleted, then records the follow; the store
// refuses it across a block and at the cap.
func (s *Account) Follow(ctx context.Context, follower, followed uuid.UUID) error {
	// E5
	if follower == followed {
		return domain.ErrSelfFollow
	}
	// E2
	status, err := s.Status(ctx, follower)
	if err != nil {
		return err
	}
	if r := status.Refusal(); r != nil {
		return r
	}
	// E1
	other, err := s.load(ctx, followed)
	if err != nil {
		return err
	}
	// E6
	if other.StoredStatus() == domain.StatusDeleted {
		return domain.ErrAccountDeleted
	}
	// A2. E3 across a block, E4 at the cap.
	if err = s.d.Follows.Follow(ctx, uuid.NewV7(), follower, followed, s.d.Now()); err != nil {
		return fmt.Errorf("follow: %w", err)
	}
	return nil
}

// Unfollow is UC-DSC-04 A2.
func (s *Account) Unfollow(ctx context.Context, follower, followed uuid.UUID) error {
	if err := s.d.Follows.Unfollow(ctx, follower, followed); err != nil {
		return fmt.Errorf("unfollow: %w", err)
	}
	return nil
}

func (s *Account) Following(ctx context.Context, follower, followed uuid.UUID) (bool, error) {
	following, err := s.d.Follows.Following(ctx, follower, followed)
	if err != nil {
		return false, fmt.Errorf("check follow: %w", err)
	}
	return following, nil
}

func (s *Account) FollowerCount(ctx context.Context, followed uuid.UUID) (int, error) {
	n, err := s.d.Follows.FollowerCount(ctx, followed)
	if err != nil {
		return 0, fmt.Errorf("count followers: %w", err)
	}
	return n, nil
}

// Follows is UC-DSC-04 A3: the accounts follower follows with their names
// and avatars, read in one query.
func (s *Account) Follows(ctx context.Context, follower uuid.UUID, p page.Request) (page.Page[FollowView], error) {
	rows, err := s.d.Follows.ListFollows(ctx, follower, p)
	if err != nil {
		return page.Page[FollowView]{}, fmt.Errorf("list follows: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, f := range rows {
		ids = append(ids, f.AccountID)
	}
	accounts, err := s.GetMany(ctx, ids)
	if err != nil {
		return page.Page[FollowView]{}, err
	}
	items := make([]FollowView, 0, len(rows))
	for _, f := range rows {
		a := accounts[f.AccountID]
		items = append(items, FollowView{ID: f.ID, AccountID: f.AccountID, DisplayName: a.DisplayName, AvatarURL: a.AvatarURL, At: f.At})
	}
	return page.Of(items, p, func(v FollowView) uuid.UUID { return v.ID }), nil
}

func (s *Account) Followers(ctx context.Context, followed uuid.UUID, p page.Request) (page.Page[Follower], error) {
	rows, err := s.d.Follows.ListFollowers(ctx, followed, p)
	if err != nil {
		return page.Page[Follower]{}, fmt.Errorf("list followers: %w", err)
	}
	items := make([]Follower, 0, len(rows))
	for _, f := range rows {
		items = append(items, Follower{ID: f.ID, AccountID: f.AccountID})
	}
	return page.Of(items, p, func(v Follower) uuid.UUID { return v.ID }), nil
}
