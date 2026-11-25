package service

import (
	"context"
	"fmt"
	"uuid"
)

func (s *Deals) OpenOrders(ctx context.Context, account uuid.UUID) (bool, error) {
	open, err := s.d.Deals.OpenOrders(ctx, account)
	if err != nil {
		return false, fmt.Errorf("check open orders: %w", err)
	}
	return open, nil
}

// AccountDeleted reads the account's active deals and ends what each holds
// open. A deal another write changed meanwhile fails the call, and the
// worker's retry reads it again; a repeated call finds nothing to change.
// A cancelled order releases nothing in catalog: only a shipped order
// reserves its listing.
func (s *Deals) AccountDeleted(ctx context.Context, account uuid.UUID) error {
	deals, err := s.d.Deals.ActiveOf(ctx, account)
	if err != nil {
		return fmt.Errorf("list deals: %w", err)
	}
	now := s.d.Now()
	for i := range deals {
		if !deals[i].PartyLeft(now) {
			continue
		}
		if err = s.d.Deals.Save(ctx, &deals[i], uuid.NewV7(), uuid.Nil(), moved(deals[i].ID(), now)); err != nil {
			return fmt.Errorf("save deal: %w", err)
		}
	}
	return nil
}

func (s *Deals) ScrubRecipients(ctx context.Context, account uuid.UUID) error {
	if err := s.d.Deals.ScrubRecipients(ctx, account); err != nil {
		return fmt.Errorf("scrub recipients: %w", err)
	}
	return nil
}
