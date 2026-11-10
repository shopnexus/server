package stt_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server2/internal/provider/stt"
)

func TestTranscribe(t *testing.T) {
	t.Parallel()
	audio := bytes.Repeat([]byte("mp4!"), 500)
	for _, key := range []string{"k", ""} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wantAuth := ""
			if key != "" {
				wantAuth = "Bearer " + key
			}
			if r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != wantAuth ||
				r.FormValue("model") != "stt" || r.FormValue("language") != "vi" || r.FormValue("response_format") != "json" {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			f, _, err := r.FormFile("file")
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			got, _ := io.ReadAll(f)
			if !bytes.Equal(got, audio) {
				http.Error(w, "wrong file", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"text":"mình bán bốn triệu rưỡi","duration":5.2,"usage":{"type":"duration","seconds":6}}`))
		}))
		c := stt.OpenAI{BaseURL: srv.URL + "/v1", APIKey: key, Model: "stt", HTTP: srv.Client()}
		text, err := c.Transcribe(context.Background(), bytes.NewReader(audio), int64(len(audio)))
		srv.Close()
		if err != nil || text != "mình bán bốn triệu rưỡi" {
			t.Fatalf("key %q: %q %v", key, text, err)
		}
	}
}

func TestTranscribeErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   error // nil: a plain error
	}{
		{"no audio", 400, `{"error":{"code":"unsupported_media"}}`, stt.ErrNoAudio},
		{"too long", 413, `{"error":{"code":"too_long"}}`, stt.ErrRefused},
		{"no access", 403, `{"error":{"message":"key_model_access_denied"}}`, stt.ErrRefused},
		{"budget", 429, `{}`, nil},
		{"down", 502, ``, nil},
		{"bad reply", 200, `{"text":`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			_, err := stt.OpenAI{BaseURL: srv.URL, Model: "stt", HTTP: srv.Client()}.
				Transcribe(context.Background(), bytes.NewReader([]byte("x")), 1)
			if err == nil {
				t.Fatal("no error")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
			if c.want == nil && (errors.Is(err, stt.ErrNoAudio) || errors.Is(err, stt.ErrRefused)) {
				t.Fatalf("%v is terminal, want a retry", err)
			}
		})
	}
}
