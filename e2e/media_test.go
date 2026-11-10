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
	res := call(t, "POST", "/v1/uploads", map[string]any{"content_type": "image/jpeg", "size": len(body)}, bearer(token))
	ticket := expect[uploadTicket](t, res, http.StatusCreated)
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
	for _, ct := range []string{"video/mp4", "video/quicktime"} {
		res := call(t, "POST", "/v1/uploads", map[string]any{"content_type": ct, "size": 52428800}, bearer(acc.AccessToken))
		if ticket := expect[uploadTicket](t, res, http.StatusCreated); ticket.Headers["Content-Type"] != ct {
			t.Errorf("%s: %+v", ct, ticket)
		}
	}
	for name, body := range map[string]map[string]any{
		"heic":            {"content_type": "image/heic", "size": 10},
		"webm":            {"content_type": "video/webm", "size": 10},
		"photo too large": {"content_type": "image/jpeg", "size": 15728641},
		"video too large": {"content_type": "video/mp4", "size": 52428801},
	} {
		if res := call(t, "POST", "/v1/uploads", body, bearer(acc.AccessToken)); res.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	expectStatus(t, call(t, "POST", "/v1/uploads", map[string]any{"content_type": "image/png", "size": 1}), http.StatusUnauthorized)
}
