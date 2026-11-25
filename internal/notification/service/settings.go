package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/notification/domain"
)

func (s *Notification) Settings(ctx context.Context, account uuid.UUID) (domain.Settings, error) {
	st, err := s.d.Settings.Get(ctx, account)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("read settings: %w", err)
	}
	return st, nil
}

func (s *Notification) PutSettings(ctx context.Context, account uuid.UUID, st domain.Settings) error {
	if err := st.Check(); err != nil {
		return err
	}
	if err := s.d.Settings.Put(ctx, account, st); err != nil {
		return fmt.Errorf("put settings: %w", err)
	}
	return nil
}
