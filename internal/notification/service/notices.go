package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/notification/repository"
	"github.com/shopnexus/server/internal/platform/page"
)

// List is UC-SHR-03, step 6: a page of the account's notices and the
// unread count.
func (s *Notification) List(ctx context.Context, account uuid.UUID, p page.Request, tag locale.Tag) (List, error) {
	rows, err := s.d.Notices.List(ctx, account, p)
	if err != nil {
		return List{}, fmt.Errorf("list notices: %w", err)
	}
	unread, err := s.d.Notices.Unread(ctx, account)
	if err != nil {
		return List{}, fmt.Errorf("count unread: %w", err)
	}
	items := make([]Item, len(rows))
	for i := range rows {
		r := &rows[i]
		items[i] = Item{
			ID: r.ID, Type: r.Type, Text: domain.Text(r.Type, r.Params, tag), Target: domain.TargetOf(r.Type, r.Params),
			Unread: !r.Read, At: r.BumpedAt,
		}
	}
	pg := page.Of(items, p, func(it Item) uuid.UUID { return it.ID })
	return List{Items: pg.Items, NextCursor: pg.NextCursor, Unread: unread}, nil
}

// Unread is UC-SHR-03, step 6: the count the app's badges show.
func (s *Notification) Unread(ctx context.Context, account uuid.UUID) (int, error) {
	n, err := s.d.Notices.Unread(ctx, account)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}

// Read is UC-SHR-03, step 6: the notice the person opened is read.
func (s *Notification) Read(ctx context.Context, account, id uuid.UUID) error {
	err := s.d.Notices.Read(ctx, account, id, s.d.Now())
	// E1: no such notice of the account's.
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read notice: %w", err)
	}
	return nil
}

// ReadAll is UC-SHR-03 A13: it returns the IDs it changed, for an undo.
func (s *Notification) ReadAll(ctx context.Context, account uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.d.Notices.ReadAll(ctx, account, s.d.Now())
	if err != nil {
		return nil, fmt.Errorf("read all: %w", err)
	}
	return ids, nil
}

// MarkUnread is UC-SHR-03 A14.
func (s *Notification) MarkUnread(ctx context.Context, account uuid.UUID, ids []uuid.UUID) error {
	if err := s.d.Notices.MarkUnread(ctx, account, ids); err != nil {
		return fmt.Errorf("mark unread: %w", err)
	}
	return nil
}

// PutToken is UC-SHR-03 A7: the token moves to the account's session.
func (s *Notification) PutToken(ctx context.Context, session, account uuid.UUID, token string) error {
	token = strings.TrimSpace(token)
	// E2: an empty or too long token.
	if token == "" || len(token) > domain.MaxToken {
		return domain.ErrInvalidToken
	}
	// E4: an account deleted meanwhile is accountdomain.ErrSessionRevoked.
	if err := s.d.Notices.PutToken(ctx, session, account, token, s.d.Now()); err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	return nil
}

// Record is UC-SHR-03, step 3: it writes or collapses the notice and
// prunes expired ones.
func (s *Notification) Record(ctx context.Context, event, fresh uuid.UUID, n domain.Notice) (Recorded, error) {
	// A1, A6: a redelivered event writes nothing; an event a newer notice
	// with its key outran comes back late.
	id, late, err := s.d.Notices.Add(ctx, event, fresh, n, n.At.Add(-domain.Retention))
	if err != nil {
		return Recorded{}, fmt.Errorf("record notice: %w", err)
	}
	return Recorded{ID: id, Late: late}, nil
}

// Tokens is UC-SHR-03, step 4: where the account's pushes go.
func (s *Notification) Tokens(ctx context.Context, account uuid.UUID) ([]Token, error) {
	out, err := s.d.Notices.Tokens(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("read tokens: %w", err)
	}
	return out, nil
}

// DeleteToken is UC-SHR-03 A7, A10: sign-out, an ended session or a dead
// token.
func (s *Notification) DeleteToken(ctx context.Context, session uuid.UUID) error {
	if err := s.d.Notices.DeleteToken(ctx, session); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return nil
}

func (s *Notification) Forget(ctx context.Context, account uuid.UUID) error {
	if err := s.d.Notices.Forget(ctx, account); err != nil {
		return fmt.Errorf("forget: %w", err)
	}
	return nil
}
