package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/notification/domain"
)

// Settings is UC-SHR-03 A8: the defaults when the account never saved any.
func (s *Notification) Settings(ctx context.Context, account uuid.UUID) (domain.Settings, error) {
	st, err := s.d.Settings.Get(ctx, account)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("read settings: %w", err)
	}
	return st, nil
}

// PutSettings is UC-SHR-03 A8: it replaces the whole document.
func (s *Notification) PutSettings(ctx context.Context, account uuid.UUID, st domain.Settings) error {
	// E3: quiet hours that start when they end, or not HH:MM.
	if err := st.Check(); err != nil {
		return err
	}
	// E4: an account deleted meanwhile is accountdomain.ErrSessionRevoked.
	if err := s.d.Settings.Put(ctx, account, st); err != nil {
		return fmt.Errorf("put settings: %w", err)
	}
	return nil
}
