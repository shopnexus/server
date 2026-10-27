package e2e

import (
	"bytes"
	"image"
	"io"
	"net/http"
	"testing"
)

// setAvatar uploads a photo as the account with token and makes it the
// avatar, returning the avatar's URL.
func setAvatar(t *testing.T, token string) string {
	t.Helper()
	res := call(t, server, "PATCH", "/v1/accounts/me", map[string]any{"avatar_upload_id": upload(t, token, photoWithGPS(t))}, bearer(token))
	v := decode[accountView](t, res)
	if res.Status != http.StatusOK || v.AvatarURL == "" {
		t.Fatalf("avatar: %d %s", res.Status, res.Body)
	}
	return v.AvatarURL
}

// The PATCH answers with the processed avatar: a square JPEG without EXIF.
func TestAvatar(t *testing.T) {
	t.Parallel()
	expect := func(name string, res Response, status int, code string) {
		t.Helper()
		if res.Status != status || (code != "" && errorOf(t, res).Code != code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	acc, _ := newAccount(t)
	patch := func(body any) Response {
		t.Helper()
		return call(t, server, "PATCH", "/v1/accounts/me", body, bearer(acc.AccessToken))
	}
	url := setAvatar(t, acc.AccessToken)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if res.StatusCode != http.StatusOK || err != nil || cfg.Width != cfg.Height || hasExif(b) {
		t.Fatalf("%s: %d %dx%d exif %v %v", url, res.StatusCode, cfg.Width, cfg.Height, hasExif(b), err)
	}
	if v := decode[accountView](t, call(t, server, "GET", "/v1/accounts/me", nil, bearer(acc.AccessToken))); v.AvatarURL != url {
		t.Fatalf("read back %q", v.AvatarURL)
	}

	other, _ := newAccount(t)
	expect("someone else's upload", patch(map[string]any{"avatar_upload_id": upload(t, other.AccessToken, photoWithGPS(t))}), 422, "invalid_upload")
	expect("not a photo", patch(map[string]any{"avatar_upload_id": upload(t, acc.AccessToken, []byte("hello, not a photo"))}), 422, "invalid_upload")
	expect("a bad id", patch(map[string]any{"avatar_upload_id": "x"}), 400, "")

	res2 := patch(map[string]any{"avatar_upload_id": nil})
	if v := decode[accountView](t, res2); res2.Status != http.StatusOK || v.AvatarURL != "" {
		t.Fatalf("remove: %d %s", res2.Status, res2.Body)
	}
}

// The seller's avatar shows in the buyer's inbox.
func TestInboxAvatar(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	avatar := setAvatar(t, seller.AccessToken)
	openChat(t, buyer.AccessToken, activeListing(t, seller.AccessToken).ID, "Còn không bạn?")
	inbox := decode[struct {
		Items []struct {
			Other struct {
				AvatarURL string `json:"avatar_url"`
			} `json:"other"`
		} `json:"items"`
	}](t, call(t, server, "GET", "/v1/screens/inbox", nil, bearer(buyer.AccessToken)))
	if len(inbox.Items) != 1 || inbox.Items[0].Other.AvatarURL != avatar {
		t.Errorf("inbox: %+v", inbox)
	}
}
