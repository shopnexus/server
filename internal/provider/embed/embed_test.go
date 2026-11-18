package embed_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server/internal/provider/embed"
)

// server answers every request with reply and keeps the last request body.
func server(t *testing.T, status int, reply string, got map[string]any, auth *string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		*auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

func TestEmbedSendsAndOrders(t *testing.T) {
	t.Parallel()
	got := map[string]any{}
	var auth string
	base := server(t, 200, `{"object":"list","data":[
{"object":"embedding","index":1,"embedding":[0,1]},
{"object":"embedding","index":0,"embedding":[1,0]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`, got, &auth)
	m := embed.OpenAI{BaseURL: base, APIKey: "k", Model: "embed-text", Dims: 2, HTTP: &http.Client{}}
	vs, err := m.Embed(context.Background(), []string{"a", "b"})
	if err != nil || len(vs) != 2 || vs[0][0] != 1 || vs[1][1] != 1 {
		t.Fatalf("%v %v", vs, err)
	}
	if got["model"] != "embed-text" || got["encoding_format"] != "float" || auth != "Bearer k" {
		t.Fatalf("request %v, auth %q", got, auth)
	}
}

func TestEmbedWithoutKeySendsNoHeader(t *testing.T) {
	t.Parallel()
	got := map[string]any{}
	auth := "unset"
	base := server(t, 200, `{"data":[{"index":0,"embedding":[1,0]}]}`, got, &auth)
	if _, err := (embed.OpenAI{BaseURL: base, Model: "m", Dims: 2, HTTP: &http.Client{}}).Embed(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Fatalf("Authorization %q without a key", auth)
	}
}

func TestEmbedRefusesBadReplies(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status  int
		reply   string
		refused bool
	}{
		"too few":      {200, `{"data":[{"index":0,"embedding":[1,0]}]}`, false},
		"wrong dims":   {200, `{"data":[{"index":0,"embedding":[1,0,0]},{"index":1,"embedding":[1,0]}]}`, false},
		"index twice":  {200, `{"data":[{"index":0,"embedding":[1,0]},{"index":0,"embedding":[1,0]}]}`, false},
		"nan":          {200, `{"data":[{"index":0,"embedding":[1,"NaN"]},{"index":1,"embedding":[1,0]}]}`, false},
		"over halfvec": {200, `{"data":[{"index":0,"embedding":[1,70000]},{"index":1,"embedding":[1,0]}]}`, false},
		"overflows":    {200, `{"data":[{"index":0,"embedding":[1,1e39]},{"index":1,"embedding":[1,0]}]}`, false},
		"not json":     {200, `<html>`, false},
		"rate":         {429, `{"error":{"message":"slow down"}}`, false},
		"down":         {503, ``, false},
		"bad input":    {400, `{"error":{"message":"image under 32 px"}}`, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := map[string]any{}
			var auth string
			base := server(t, tc.status, tc.reply, got, &auth)
			_, err := (embed.OpenAI{BaseURL: base, Model: "m", Dims: 2, HTTP: &http.Client{}}).Embed(context.Background(), []string{"a", "b"})
			if err == nil {
				t.Fatal("took a bad reply")
			}
			if errors.Is(err, embed.ErrRefused) != tc.refused {
				t.Fatalf("ErrRefused = %v for %v", !tc.refused, err)
			}
		})
	}
}

func TestImageInput(t *testing.T) {
	t.Parallel()
	if got := embed.ImageInput([]byte{0xFF, 0xD8}); got != "data:image/jpeg;base64,/9g=" {
		t.Fatal(got)
	}
}
