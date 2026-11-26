package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// Block is UC-ACC-05.
func (s *Account) Block(ctx context.Context, blocker, blocked uuid.UUID) error {
	// E1
	if blocker == blocked {
		return domain.ErrSelfBlock
	}
	// E2
	if blocked == domain.ShopNexusID {
		return domain.ErrSystemAccount
	}
	// E4
	other, err := s.load(ctx, blocked)
	if err != nil {
		return err
	}
	// E3
	if other.StoredStatus() == domain.StatusDeleted {
		return domain.ErrAccountDeleted
	}
	// 2. Block and drop the follows both ways · BR-ACC-01; A1 a block that stands changes nothing.
	if err = s.d.Accounts.Block(ctx, uuid.NewV7(), blocker, blocked, s.d.Now()); err != nil {
		return fmt.Errorf("block: %w", err)
	}
	return nil
}

// Unblock is UC-ACC-05 A2.
func (s *Account) Unblock(ctx context.Context, blocker, blocked uuid.UUID) error {
	if err := s.d.Accounts.Unblock(ctx, blocker, blocked); err != nil {
		return fmt.Errorf("unblock: %w", err)
	}
	return nil
}

func (s *Account) Blocked(ctx context.Context, a, b uuid.UUID) (bool, error) {
	blocked, err := s.d.Accounts.Blocked(ctx, a, b)
	if err != nil {
		return false, fmt.Errorf("check block: %w", err)
	}
	return blocked, nil
}

func (s *Account) BlockedWith(ctx context.Context, account uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.d.Accounts.BlockedWith(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("read blocks: %w", err)
	}
	return ids, nil
}

// Blocks lists the accounts blocker blocks with their display names, read
// in one query.
//
// Blocks is UC-ACC-05 A2.
func (s *Account) Blocks(ctx context.Context, blocker uuid.UUID, p page.Request) (page.Page[BlockView], error) {
	rows, err := s.d.Accounts.ListBlocks(ctx, blocker, p)
	if err != nil {
		return page.Page[BlockView]{}, fmt.Errorf("list blocks: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, b := range rows {
		ids = append(ids, b.AccountID)
	}
	accounts, err := s.GetMany(ctx, ids)
	if err != nil {
		return page.Page[BlockView]{}, err
	}
	items := make([]BlockView, 0, len(rows))
	for _, b := range rows {
		items = append(items, BlockView{ID: b.ID, AccountID: b.AccountID, DisplayName: accounts[b.AccountID].DisplayName, At: b.At})
	}
	return page.Of(items, p, func(v BlockView) uuid.UUID { return v.ID }), nil
}
