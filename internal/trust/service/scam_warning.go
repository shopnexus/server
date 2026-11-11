package service

import (
	"context"
	"fmt"
	"uuid"
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

func (t *Trust) DismissScamWarning(ctx context.Context, account, conversation uuid.UUID) error {
	if _, err := t.chats.Get(ctx, account, conversation); err != nil {
		return fmt.Errorf("read conversation: %w", err)
	}
	if _, err := t.scams.Dismiss(ctx, conversation, account, t.now(), uuid.NewV7()); err != nil {
		return fmt.Errorf("dismiss scam warning: %w", err)
	}
	return nil
}

func (t *Trust) DeleteScamWarnings(ctx context.Context, account uuid.UUID) error {
	if err := t.scams.DeleteAccount(ctx, account); err != nil {
		return fmt.Errorf("delete scam warnings: %w", err)
	}
	return nil
}
