package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"
)

func (t *Trust) ScamWarnings(ctx context.Context, account uuid.UUID, conversations []uuid.UUID, tag locale.Tag) (map[uuid.UUID]ScamWarningView, error) {
	if len(conversations) == 0 {
		return map[uuid.UUID]ScamWarningView{}, nil
	}
	ws, err := t.d.Scams.Of(ctx, account, conversations)
	if err != nil {
		return nil, fmt.Errorf("read scam warnings: %w", err)
	}
	out := make(map[uuid.UUID]ScamWarningView, len(ws))
	for id, w := range ws {
		v := ScamWarningView{Signals: []ScamSignalView{}, Dismissed: w.Dismissed()}
		for _, s := range w.Signals() {
			v.Signals = append(v.Signals, ScamSignalView{Code: string(s.Code), Params: s.Params, Text: s.Text(tag), MessageID: s.MessageID})
		}
		out[id] = v
	}
	return out, nil
}

func (t *Trust) DismissScamWarning(ctx context.Context, account, conversation uuid.UUID) error {
	if _, err := t.d.Chats.Get(ctx, account, conversation); err != nil {
		return fmt.Errorf("read conversation: %w", err)
	}
	if _, err := t.d.Scams.Dismiss(ctx, conversation, account, t.d.Now(), uuid.NewV7()); err != nil {
		return fmt.Errorf("dismiss scam warning: %w", err)
	}
	return nil
}
