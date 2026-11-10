// Package email sends email. Callers write the subject and body.
package email

import (
	"context"
	"errors"
	"log/slog"
)

// ErrRefused is a message the provider will never deliver, such as to an
// address it rejects; sending it again fails again.
var ErrRefused = errors.New("email: refused")

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
