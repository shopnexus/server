package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

func (s *Account) Block(ctx context.Context, blocker, blocked uuid.UUID) error {
	if blocker == blocked {
		return domain.ErrSelfBlock
	}
	if _, err := s.Get(ctx, blocked); err != nil {
		return err
	}
	if err := s.accounts.Block(ctx, uuid.NewV7(), blocker, blocked, s.now()); err != nil {
		return fmt.Errorf("block: %w", err)
	}
	return nil
}

func (s *Account) Unblock(ctx context.Context, blocker, blocked uuid.UUID) error {
	if err := s.accounts.Unblock(ctx, blocker, blocked); err != nil {
		return fmt.Errorf("unblock: %w", err)
	}
	return nil
}

func (s *Account) Blocked(ctx context.Context, a, b uuid.UUID) (bool, error) {
	blocked, err := s.accounts.Blocked(ctx, a, b)
	if err != nil {
		return false, fmt.Errorf("check block: %w", err)
	}
	return blocked, nil
}

// Blocks lists the accounts blocker blocks with their display names.
func (s *Account) Blocks(ctx context.Context, blocker uuid.UUID, p page.Request) (page.Page[BlockView], error) {
	rows, err := s.accounts.ListBlocks(ctx, blocker, p)
	if err != nil {
		return page.Page[BlockView]{}, fmt.Errorf("list blocks: %w", err)
	}
	items := make([]BlockView, 0, len(rows))
	for _, b := range rows {
		a, getErr := s.Get(ctx, b.AccountID)
		if getErr != nil {
			return page.Page[BlockView]{}, getErr
		}
		items = append(items, BlockView{ID: b.ID, AccountID: b.AccountID, DisplayName: a.DisplayName, At: b.At})
	}
	return page.Of(items, p, func(v BlockView) uuid.UUID { return v.ID }), nil
}
