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

// Message is one mail to one address. Key, when set, names the message: a
// provider that honours it sends a message with a key it saw in the last
// day only once, so a retried step does not mail twice.
type Message struct {
	To      string
	Subject string
	Text    string
	Key     string
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender writes messages to the log instead of sending them; APP_ENV=prod
// refuses it.
type LogSender struct{ Log *slog.Logger }

func (s LogSender) Send(ctx context.Context, m Message) error {
	s.Log.InfoContext(ctx, "email (log sender)", "to", m.To, "subject", m.Subject, "body", m.Text)
	return nil
}
