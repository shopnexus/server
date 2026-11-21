package indexnow_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/shopnexus/server/internal/provider/indexnow"
)

// Submit posts IndexNow's JSON body; 200 and 202 succeed, and only 429 and
// 5xx are worth another try.
func TestSubmit(t *testing.T) {
	t.Parallel()
	for status, retry := range map[int]bool{
		http.StatusOK: false, http.StatusAccepted: false, http.StatusForbidden: false, http.StatusUnprocessableEntity: false,
		http.StatusTooManyRequests: true, http.StatusServiceUnavailable: true,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json; charset=utf-8" {
					t.Errorf("%s %s", r.Method, r.Header.Get("Content-Type"))
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)
			err := indexnow.New(srv.Client(), srv.URL).Submit(context.Background(), indexnow.Submission{
				Host: "shop.example", Key: "0123456789abcdef", KeyLocation: "https://shop.example/0123456789abcdef.txt",
				URLs: []string{"https://shop.example/listings/a-1"},
			})
			if got["host"] != "shop.example" || got["keyLocation"] != "https://shop.example/0123456789abcdef.txt" ||
				!slices.Equal(got["urlList"].([]any), []any{"https://shop.example/listings/a-1"}) {
				t.Fatalf("body %v", got)
			}
			se, isStatus := errors.AsType[*indexnow.StatusError](err)
			switch {
			case status == http.StatusOK || status == http.StatusAccepted:
				if err != nil {
					t.Fatal(err)
				}
			case !isStatus || se.Status != status || se.Retry() != retry:
				t.Fatalf("%v, want status %d retry %v", err, status, retry)
			default:
			}
		})
	}
}

func TestSubmitRefusesAnEmptyOrLongList(t *testing.T) {
	t.Parallel()
	c := indexnow.New(http.DefaultClient, "http://127.0.0.1:1")
	if err := c.Submit(context.Background(), indexnow.Submission{}); err == nil {
		t.Fatal("no URLs were taken")
	}
	if err := c.Submit(context.Background(), indexnow.Submission{URLs: make([]string, indexnow.MaxURLs+1)}); err == nil {
		t.Fatal("too many URLs were taken")
	}
}
