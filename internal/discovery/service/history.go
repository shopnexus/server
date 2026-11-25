package service

import (
	"context"
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"
)

// HistoryView is the SearchHistory schema.
type HistoryView struct {
	Items []HistoryItem `json:"items"`
}

// HistoryItem is the SearchHistoryItem schema.
type HistoryItem struct {
	ID         uuid.UUID `json:"id"`
	Query      string    `json:"query"`
	Title      string    `json:"title"`
	Category   string    `json:"category,omitempty"`
	PhotoURL   string    `json:"photo_url,omitempty"`
	SearchedAt time.Time `json:"searched_at"`
}

func (d *Discovery) History(ctx context.Context, account uuid.UUID) (HistoryView, error) {
	entries, err := d.d.History.List(ctx, account)
	if err != nil {
		return HistoryView{}, fmt.Errorf("read history: %w", err)
	}
	out := HistoryView{Items: make([]HistoryItem, 0, len(entries))}
	for _, e := range entries {
		first, size := utf8.DecodeRuneInString(e.Query)
		out.Items = append(out.Items, HistoryItem{
			ID: e.ID, Query: e.Query, Title: string(unicode.ToUpper(first)) + e.Query[size:],
			Category: e.Category, PhotoURL: e.PhotoURL, SearchedAt: e.SearchedAt,
		})
	}
	return out, nil
}

func (d *Discovery) ForgetSearch(ctx context.Context, account, id uuid.UUID) error {
	if err := d.d.History.Forget(ctx, account, id); err != nil {
		return fmt.Errorf("forget search: %w", err)
	}
	return nil
}

func (d *Discovery) ClearHistory(ctx context.Context, account uuid.UUID) error {
	if err := d.d.History.Clear(ctx, account); err != nil {
		return fmt.Errorf("clear history: %w", err)
	}
	return nil
}

func (d *Discovery) AccountDeleted(ctx context.Context, account uuid.UUID) error {
	if err := d.d.Saves.Clear(ctx, account); err != nil {
		return fmt.Errorf("clear saves: %w", err)
	}
	if err := d.d.History.Clear(ctx, account); err != nil {
		return fmt.Errorf("clear history: %w", err)
	}
	if err := d.d.Hidden.Clear(ctx, account); err != nil {
		return fmt.Errorf("clear hides: %w", err)
	}
	if err := d.d.Watches.Clear(ctx, account); err != nil {
		return fmt.Errorf("clear watches: %w", err)
	}
	if err := d.d.Views.Clear(ctx, account); err != nil {
		return fmt.Errorf("clear views: %w", err)
	}
	if err := d.d.ModelUses.Clear(ctx, account); err != nil {
		return fmt.Errorf("clear model uses: %w", err)
	}
	return nil
}
