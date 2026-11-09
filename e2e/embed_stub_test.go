package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
)

// stubEmbed stands in for the embedding aliases: embed-text answers 1024
// dimensions, embed-image 1152, each input its embedtest vector. Hashed
// vectors are close to orthogonal, so below the e2e floors of 0.5: vector
// lists stay empty unless a test sets a vector, and BM25 answers.
type stubEmbed struct {
	srv         *httptest.Server
	text, image *embedtest.Model
}

func newEmbed() *stubEmbed {
	e := &stubEmbed{text: embedtest.New(catalogdomain.TextDims), image: embedtest.New(catalogdomain.PhotoDims)}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if r.URL.Path != "/v1/embeddings" || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		m := e.text
		if req.Model == "embed-image" {
			m = e.image
			if slices.ContainsFunc(req.Input, unavailable) {
				http.Error(w, "the stub is down for this photo", http.StatusServiceUnavailable)
				return
			}
		}
		vs, _ := m.Embed(context.Background(), req.Input)
		data := make([]map[string]any, len(vs))
		for i, v := range vs {
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": v}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	return e
}

func (e *stubEmbed) url() string { return e.srv.URL + "/v1" }

// unavailable reports a data-URL JPEG whose middle pixel is magenta, the
// photo magentaPhoto makes.
func unavailable(input string) bool {
	b64, ok := strings.CutPrefix(input, "data:image/jpeg;base64,")
	if !ok {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return false
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	b := img.Bounds()
	r, g, bl, _ := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	return r>>8 > 200 && g>>8 < 60 && bl>>8 > 200
}
