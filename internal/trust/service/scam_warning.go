package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/trust/repository"
)

func (t *Trust) ScamWarnings(ctx context.Context, account uuid.UUID, conversations []uuid.UUID) (map[uuid.UUID]ScamWarningView, error) {
	if len(conversations) == 0 {
		return map[uuid.UUID]ScamWarningView{}, nil
	}
	ws, err := t.scams.Of(ctx, account, conversations)
	if err != nil {
		return nil, fmt.Errorf("read scam warnings: %w", err)
	}
	out := make(map[uuid.UUID]ScamWarningView, len(ws))
	for id, w := range ws {
		v := ScamWarningView{Signals: []ScamSignalView{}, Dismissed: w.Dismissed()}
		for _, s := range w.Signals() {
			v.Signals = append(v.Signals, ScamSignalView{Code: string(s.Code), Text: s.Text()})
		}
		out[id] = v
	}
	return out, nil
}

// DismissScamWarning reads the warning again once when a check saved it
// meanwhile, then answers try_again.
func (t *Trust) DismissScamWarning(ctx context.Context, account, conversation uuid.UUID) error {
	if _, err := t.chats.Get(ctx, account, conversation); err != nil {
		return fmt.Errorf("read conversation: %w", err)
	}
	for range 2 {
		w, err := t.scams.Get(ctx, conversation, account)
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read scam warning: %w", err)
		}
		if !w.Dismiss(t.now()) {
			return nil
		}
		err = t.scams.Save(ctx, &w, uuid.NewV7())
		if err == nil {
			return nil
		}
		if !errors.Is(err, repository.ErrConflict) {
			return fmt.Errorf("save scam warning: %w", err)
		}
	}
	return apperr.ErrTryAgain
}

func (t *Trust) DeleteScamWarnings(ctx context.Context, account uuid.UUID) error {
	if err := t.scams.DeleteAccount(ctx, account); err != nil {
		return fmt.Errorf("delete scam warnings: %w", err)
	}
	return nil
}
