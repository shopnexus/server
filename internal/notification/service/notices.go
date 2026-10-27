package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/notification/repository"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/page"
)

func (s *Notification) List(ctx context.Context, account uuid.UUID, p page.Request) (List, error) {
	rows, err := s.notices.List(ctx, account, p)
	if err != nil {
		return List{}, fmt.Errorf("list notices: %w", err)
	}
	unread, err := s.notices.Unread(ctx, account)
	if err != nil {
		return List{}, fmt.Errorf("count unread: %w", err)
	}
	items := make([]Item, len(rows))
	for i, r := range rows {
		items[i] = Item{
			ID: r.ID, Type: r.Type, Text: domain.Text(r.Type, r.Params), Target: domain.TargetOf(r.Type, r.Params),
			Unread: !r.Read, At: r.BumpedAt,
		}
	}
	pg := page.Of(items, p, func(it Item) uuid.UUID { return it.ID })
	return List{Items: pg.Items, NextCursor: pg.NextCursor, Unread: unread}, nil
}

func (s *Notification) Unread(ctx context.Context, account uuid.UUID) (int, error) {
	n, err := s.notices.Unread(ctx, account)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}

func (s *Notification) Read(ctx context.Context, account, id uuid.UUID) error {
	err := s.notices.Read(ctx, account, id, s.now())
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read notice: %w", err)
	}
	return nil
}

func (s *Notification) ReadAll(ctx context.Context, account uuid.UUID) error {
	if err := s.notices.ReadAll(ctx, account, s.now()); err != nil {
		return fmt.Errorf("read all: %w", err)
	}
	return nil
}

func (s *Notification) PutToken(ctx context.Context, p authn.Principal, token string) error {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > domain.MaxToken {
		return domain.ErrInvalidToken
	}
	if err := s.notices.PutToken(ctx, p.SessionID, p.AccountID, token, s.now()); err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	return nil
}

func (s *Notification) DeleteToken(ctx context.Context, p authn.Principal) error {
	if err := s.notices.DeleteToken(ctx, p.SessionID); err != nil {
		return fmt.Errorf("forget token: %w", err)
	}
	return nil
}

func (s *Notification) Record(ctx context.Context, event uuid.UUID, n domain.Notice) (uuid.UUID, error) {
	id, err := s.notices.Add(ctx, event, n, n.At.Add(-domain.Retention))
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("record notice: %w", err)
	}
	return id, nil
}

func (s *Notification) Tokens(ctx context.Context, account uuid.UUID) ([]Token, error) {
	rows, err := s.notices.Tokens(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("read tokens: %w", err)
	}
	out := make([]Token, len(rows))
	for i, r := range rows {
		out[i] = Token{Session: r.Session, Token: r.Token}
	}
	return out, nil
}

func (s *Notification) ForgetToken(ctx context.Context, session uuid.UUID) error {
	if err := s.notices.DeleteToken(ctx, session); err != nil {
		return fmt.Errorf("forget token: %w", err)
	}
	return nil
}
