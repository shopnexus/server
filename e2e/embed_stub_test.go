package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/ai/aitest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/embed/embedtest"
)

// stubEmbed stands in for the alias embed-text, 1024 dimensions, and for
// module ai's photo vectors, 1152, each input its embedtest vector of the
// photo's data URL. Hashed vectors are close to orthogonal, so below the
// e2e floors of 0.5: vector lists stay empty unless a test sets a vector,
// and BM25 answers.
type stubEmbed struct {
	srv         *httptest.Server
	text, image *embedtest.Model
}

func newEmbed() *stubEmbed {
	e := &stubEmbed{text: embedtest.New(catalogdomain.TextDims), image: embedtest.New(catalogdomain.PhotoDims)}
	// The stub answers' drafts read as their categories, on axes no other
	// test sets.
	bicycle, iphone := embedtest.Axis(catalogdomain.TextDims, 1000), embedtest.Axis(catalogdomain.TextDims, 1001)
	for text, v := range map[string][]float32{
		"Xe cộ › Xe đạp":      bicycle,
		"Điện thoại › iPhone": iphone,
		catalogdomain.GuessText("Xe đạp Asama xanh", "Xe đi ít, giá một triệu hai, có thương lượng."): bicycle,
		catalogdomain.GuessText("Xe đạp Asama xanh", "Xe đi ít, có thương lượng."):                    bicycle,
		catalogdomain.GuessText("Xe đạp Asama xanh", "Xe đạp Asama màu xanh, có giỏ trước."):          bicycle,
		catalogdomain.GuessText("iPhone 11 64GB Trắng, pin 81%", "Máy dùng 3 năm, zin all. Liên hệ."): iphone,
	} {
		e.text.Set(text, v)
	}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if r.URL.Path != "/v1/embeddings" || json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != "embed-text" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		vs, _ := e.text.Embed(context.Background(), req.Input)
		data := make([]map[string]any, len(vs))
		for i, v := range vs {
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": v}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	return e
}

// photos is module ai's SigLIP 2; it fails for the magenta photo.
func (e *stubEmbed) photos() aitest.Fake {
	return aitest.Fake{Embed: func(images [][]byte) ([][]float32, error) {
		inputs := make([]string, len(images))
		for i, img := range images {
			inputs[i] = embed.ImageInput(img)
		}
		if slices.ContainsFunc(inputs, unavailable) {
			return nil, errors.New("the stub is down for this photo")
		}
		return e.image.Embed(context.Background(), inputs)
	}}
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

// magentaPhoto is a JPEG the stub's photo vectors fail for.
func magentaPhoto(t *testing.T) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			m.Set(x, y, color.RGBA{255, 0, 255, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
