package frames_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server2/internal/provider/frames"
)

func TestExtract(t *testing.T) {
	t.Parallel()
	video := bytes.Repeat([]byte("mp4!"), 1000)
	jpeg := []byte{0xff, 0xd8, 0xff, 0xd9}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/video/frames" || r.ContentLength <= int64(len(video)) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		got, _ := io.ReadAll(f)
		if !bytes.Equal(got, video) {
			http.Error(w, "wrong file", http.StatusBadRequest)
			return
		}
		b64 := base64.StdEncoding.EncodeToString(jpeg)
		_, _ = w.Write([]byte(`{"duration":27.4,"frames":[{"at_seconds":1.5,"sharpness":412.3,"jpeg":"` + b64 + `"},` +
			`{"at_seconds":4,"sharpness":300,"jpeg":"` + b64 + `"}]}`))
	}))
	defer srv.Close()
	c := frames.Client{BaseURL: srv.URL + "/v1/", HTTP: srv.Client()}
	res, err := c.Extract(context.Background(), bytes.NewReader(video), int64(len(video)))
	if err != nil {
		t.Fatal(err)
	}
	if res.Duration != 27.4 || len(res.Frames) != 2 || res.Frames[0].AtSeconds != 1.5 || res.Frames[1].Sharpness != 300 ||
		!bytes.Equal(res.Frames[0].JPEG, jpeg) {
		t.Fatalf("%+v", res)
	}
}

func TestExtractErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		status   int
		body     string
		want     error // nil: a plain error, not one of the sentinels
		terminal bool
	}{
		{"no video", 400, `{"error":{"message":"m","type":"invalid_request_error","code":"unsupported_media"}}`, frames.ErrUnreadable, true},
		{"too short", 400, `{"error":{"code":"too_short"}}`, frames.ErrTooShort, true},
		{"too long", 413, `{"error":{"code":"too_long"}}`, frames.ErrTooLong, true},
		{"too large", 413, `{"error":{"code":"too_large"}}`, frames.ErrRefused, true},
		{"bad form", 400, `{"error":{"code":"invalid_request"}}`, frames.ErrRefused, true},
		{"not json", 404, `nope`, frames.ErrRefused, true},
		{"busy", 429, `{}`, nil, false},
		{"down", 500, `{"error":{"code":"internal_error"}}`, nil, false},
		{"bad reply", 200, `{"frames":`, nil, false},
		{"empty jpeg", 200, `{"duration":5,"frames":[{"at_seconds":1,"sharpness":1,"jpeg":""}]}`, nil, false},
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
			_, err := frames.Client{BaseURL: srv.URL, HTTP: srv.Client()}.Extract(context.Background(), bytes.NewReader([]byte("x")), 1)
			if err == nil {
				t.Fatal("no error")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
			terminal := errors.Is(err, frames.ErrUnreadable) || errors.Is(err, frames.ErrTooShort) ||
				errors.Is(err, frames.ErrTooLong) || errors.Is(err, frames.ErrRefused)
			if terminal != c.terminal {
				t.Fatalf("%v: terminal %v, want %v", err, terminal, c.terminal)
			}
		})
	}
}

// A reader that holds fewer bytes than size fails before ai answers.
func TestExtractWrongSize(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"duration":5,"frames":[]}`))
	}))
	defer srv.Close()
	_, err := frames.Client{BaseURL: srv.URL, HTTP: srv.Client()}.Extract(context.Background(), bytes.NewReader([]byte("abc")), 10)
	if err == nil {
		t.Fatal("a short body was sent")
	}
}
