package email_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/provider/email"
)

func TestLogSenderWritesMessage(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	s := email.LogSender{Log: slog.New(slog.NewTextHandler(&buf, nil))}
	if err := s.Send(context.Background(), email.Message{To: "linh@example.com", Subject: "Mã xác minh ShopNexus: 583920", Text: "583920"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"linh@example.com", "583920"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("log %q lacks %q", buf.String(), want)
		}
	}
}

func TestResendPostsTheMessage(t *testing.T) {
	t.Parallel()
	var got struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}
	var auth, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, key = r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"id":"49a3999c"}`)
	}))
	t.Cleanup(srv.Close)
	s := email.NewResend(&http.Client{}, "re_test", "ShopNexus <no-reply@shopnexus.test>")
	s.URL = srv.URL
	if err := s.Send(context.Background(), email.Message{To: "linh@example.com", Subject: "Chủ đề", Text: "Nội dung", Key: "ev/0"}); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer re_test" || key != "ev/0" || got.From != "ShopNexus <no-reply@shopnexus.test>" || len(got.To) != 1 ||
		got.To[0] != "linh@example.com" || got.Subject != "Chủ đề" || got.Text != "Nội dung" {
		t.Fatalf("auth %q, key %q, body %+v", auth, key, got)
	}
}

func TestResendReportsARefusal(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"domain not verified"}`, http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	s := email.NewResend(&http.Client{}, "re_test", "a@b.test")
	s.URL = srv.URL
	err := s.Send(context.Background(), email.Message{To: "linh@example.com", Subject: "s", Text: "b"})
	if !errors.Is(err, email.ErrRefused) || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "domain not verified") {
		t.Fatalf("%v", err)
	}
}

// A 429 or a 5xx passes; a retry may succeed.
func TestResendRetryableFailures(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "later", status)
		}))
		s := email.NewResend(&http.Client{}, "re_test", "a@b.test")
		s.URL = srv.URL
		if err := s.Send(context.Background(), email.Message{To: "linh@example.com", Subject: "s", Text: "b"}); err == nil || errors.Is(err, email.ErrRefused) {
			t.Errorf("%d: %v", status, err)
		}
		srv.Close()
	}
}
