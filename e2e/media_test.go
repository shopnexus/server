package e2e

import (
	"bytes"
	"net/http"
	"testing"
)

type uploadTicket struct {
	UploadID string            `json:"upload_id"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
}

// upload asks for a JPEG ticket as the account with token and PUTs body to
// it.
func upload(t *testing.T, token string, body []byte) string {
	t.Helper()
	res := call(t, server, "POST", "/v1/uploads", map[string]any{"content_type": "image/jpeg", "size": len(body)}, bearer(token))
	if res.Status != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.Status, res.Body)
	}
	ticket := decode[uploadTicket](t, res)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, ticket.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range ticket.Headers {
		req.Header.Set(k, v)
	}
	req.ContentLength = int64(len(body))
	put, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = put.Body.Close()
	if put.StatusCode != http.StatusOK {
		t.Fatalf("put to the presigned URL: %d", put.StatusCode)
	}
	return ticket.UploadID
}

func TestCreateUpload(t *testing.T) {
	t.Parallel()
	acc, _ := newAccount(t)
	if id := upload(t, acc.AccessToken, []byte("not yet a jpeg")); id == "" {
		t.Fatal("no upload id")
	}
	for name, body := range map[string]map[string]any{
		"heic":      {"content_type": "image/heic", "size": 10},
		"too large": {"content_type": "image/jpeg", "size": 15728641},
	} {
		if res := call(t, server, "POST", "/v1/uploads", body, bearer(acc.AccessToken)); res.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	if res := call(t, server, "POST", "/v1/uploads", map[string]any{"content_type": "image/png", "size": 1}); res.Status != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", res.Status)
	}
}
