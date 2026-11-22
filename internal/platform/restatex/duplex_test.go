package restatex

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The SDK answers before it reads the request to the end, then drains it.
func TestFullDuplexKeepsTheBodyReadable(t *testing.T) {
	t.Parallel()
	drained := make(chan error, 1)
	srv := httptest.NewServer(fullDuplex(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.CopyN(io.Discard, r.Body, 1)
		_, _ = w.Write([]byte("ok"))
		w.(http.Flusher).Flush()
		_, err := io.Copy(io.Discard, r.Body)
		drained <- err
	})))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/invoke/Orders/Place", strings.NewReader("journal"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if err := <-drained; err != nil {
		t.Fatalf("drain: %v", err)
	}
}
