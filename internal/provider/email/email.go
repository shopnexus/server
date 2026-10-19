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

// SendCommand is the job that asks a consumer to send one message. The
// field is Title because Subject is the method naming the job's subject.
type SendCommand struct {
	To    string `json:"to"`
	Title string `json:"subject"`
	Body  string `json:"body"`
}

func (SendCommand) Subject() string { return "cmd.email.send" }

// LogSender writes messages to the log instead of sending them. Development
// only.
type LogSender struct{ Log *slog.Logger }

func (s LogSender) Send(ctx context.Context, to, subject, body string) error {
	s.Log.InfoContext(ctx, "email (dev sender)", "to", to, "subject", subject, "body", body)
	return nil
}
