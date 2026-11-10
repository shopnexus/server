// Package email sends email. Callers write the subject and body.
package email

import (
	"context"
	"log/slog"
)

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// LogSender writes messages to the log instead of sending them; APP_ENV=prod
// refuses it.
type LogSender struct{ Log *slog.Logger }

func (s LogSender) Send(ctx context.Context, to, subject, body string) error {
	s.Log.InfoContext(ctx, "email (log sender)", "to", to, "subject", subject, "body", body)
	return nil
}
