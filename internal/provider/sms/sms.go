// Package sms sends text messages. It knows nothing about OTP or any other
// business purpose; callers write the text.
package sms

import (
	"context"
	"log/slog"
)

// Sender delivers one text message.
type Sender interface {
	Send(ctx context.Context, to, text string) error
}

// LogSender writes messages to the log instead of sending them. Development
// only: it is the one place an OTP code may appear in a log.
type LogSender struct{ Log *slog.Logger }

func (s LogSender) Send(ctx context.Context, to, text string) error {
	s.Log.InfoContext(ctx, "sms (dev sender)", "to", to, "text", text)
	return nil
}
