package email_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/provider/email"
)

func TestLogSenderWritesMessage(t *testing.T) {
	var buf bytes.Buffer
	s := email.LogSender{Log: slog.New(slog.NewTextHandler(&buf, nil))}
	if err := s.Send(context.Background(), "linh@example.com", "Mã xác minh", "583920"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"linh@example.com", "Mã xác minh", "583920"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("log %q lacks %q", buf.String(), want)
		}
	}
	if (email.SendCommand{}).Subject() != "cmd.email.send" {
		t.Fatal("subject")
	}
}
