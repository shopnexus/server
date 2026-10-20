package sms_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/provider/sms"
)

func TestLogSenderWritesMessage(t *testing.T) {
	var buf bytes.Buffer
	s := sms.LogSender{Log: slog.New(slog.NewTextHandler(&buf, nil))}
	if err := s.Send(context.Background(), "+84901234567", "Mã của bạn: 583920"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "+84901234567") || !strings.Contains(buf.String(), "583920") {
		t.Fatalf("log %q", buf.String())
	}
}
