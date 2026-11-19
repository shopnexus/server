package e2e

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/shopnexus/server/internal/ai/aitest"
	aidomain "github.com/shopnexus/server/internal/ai/domain"
)

// The video bodies the assist stub tells apart. Any other body is a
// seller's bicycle.
const (
	shortVideo = "a two-second video"
	emptyRoom  = "a video of an empty room"
)

// The model's answers: the bicycle keeps frames 2 and 0; the empty room
// shows no item.
const (
	bicycleAnswer = `{"is_item":true,"title":"Xe đạp Asama xanh","condition":"used",` +
		`"specs":[{"label":"Màu","value":"Xanh"},{"label":"Hãng","value":"Asama"},{"label":"Loại xe","value":"Xe đạp phố"},` +
		`{"label":"Cỡ khung","value":"26 inch"}],"description":"Xe đi ít, giá một triệu hai, có thương lượng.",` +
		`"spoken_price":1200000,"negotiable":true,"kept":[2,0],` +
		`"estimate":{"low":1000000,"median":1200000,"high":1400000}}`
	iphoneLinkAnswer = `{"is_item":true,"title":"iPhone 11 64GB Trắng, pin 81%","condition":"used",` +
		`"specs":[{"label":"Dòng máy","value":"iPhone 11"},{"label":"Dung lượng","value":"64 GB"},` +
		`{"label":"Màu","value":"Trắng"},{"label":"Pin","value":"81%"}],"description":"Máy dùng 3 năm, zin all. Có thương lượng.",` +
		`"negotiable":true,"contact_removed":true,"estimate":{"low":4000000,"median":4500000,"high":5000000}}`
	flatLinkAnswer = `{"is_item":false,"title":"","condition":"used","specs":[],"description":"",` +
		`"negotiable":false,"contact_removed":false,"estimate":null}`
	bicyclePhotoAnswer = `{"is_item":true,"title":"Xe đạp Asama xanh","condition":"used",` +
		`"specs":[{"label":"Màu","value":"Xanh"},{"label":"Hãng","value":"Asama"},{"label":"Loại xe","value":"Xe đạp phố"},` +
		`{"label":"Cỡ khung","value":"26 inch"}],"description":"Xe đạp Asama màu xanh, có giỏ trước.",` +
		`"estimate":{"low":1000000,"median":1200000,"high":1400000}}`
	noItemAnswer = `{"is_item":false,"title":"","condition":"used","specs":[],"description":"",` +
		`"spoken_price":null,"negotiable":false,"kept":[],"estimate":null}`
)

// stubAssist stands in for what a video read calls: the video-read model,
// on a server, and module ai's frames and speech, in models.
type stubAssist struct{ srv *httptest.Server }

func newAssist() *stubAssist {
	a := &stubAssist{}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			var req struct {
				Messages []struct {
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
				ResponseFormat struct {
					JSONSchema struct {
						Name string `json:"name"`
					} `json:"json_schema"`
				} `json:"response_format"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Messages) != 2 {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if req.ResponseFormat.JSONSchema.Name == "photo_read" {
				writeStubJSON(w, map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": bicyclePhotoAnswer}}}})
				return
			}
			if req.ResponseFormat.JSONSchema.Name == "link_read" {
				var text string
				if json.Unmarshal(req.Messages[1].Content, &text) != nil {
					http.Error(w, "a link read sends text only", http.StatusBadRequest)
					return
				}
				answer := iphoneLinkAnswer
				if strings.Contains(text, "căn hộ") {
					answer = flatLinkAnswer
				}
				writeStubJSON(w, map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": answer}}}})
				return
			}
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(req.Messages[1].Content, &parts) != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			answer := bicycleAnswer
			for _, p := range parts {
				if p.Type == "text" && strings.Contains(p.Text, "không có gì") {
					answer = noItemAnswer
				}
			}
			writeStubJSON(w, map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": answer}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	return a
}

func (a *stubAssist) url() string { return a.srv.URL + "/v1" }

// models are module ai's frames and speech: a short video is too short,
// and the empty room has its own speech.
func (*stubAssist) models() aitest.Fake {
	return aitest.Fake{
		Cut: func(video []byte) (aidomain.FramesOutput, error) {
			if string(video) == shortVideo {
				return aidomain.FramesOutput{Duration: 2}, aidomain.ErrTooShort
			}
			out := aidomain.FramesOutput{Duration: 12, Frames: make([]aidomain.Frame, 3)}
			for i := range out.Frames {
				out.Frames[i] = aidomain.Frame{AtSeconds: float64(i) * 4, Sharpness: float64(100 + i), JPEG: frameJPEG(i)}
			}
			return out, nil
		},
		Hear: func(video []byte) (string, error) {
			if string(video) == emptyRoom {
				return "trong phòng không có gì", nil
			}
			return "mình bán xe đạp asama giá một triệu hai có thương lượng", nil
		},
	}
}

func writeStubJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// frameJPEG is frame i of the bicycle video: a 96×64 gradient, a hue of
// its own per frame.
func frameJPEG(i int) []byte {
	m := image.NewRGBA(image.Rect(0, 0, 96, 64))
	for y := range 64 {
		for x := range 96 {
			m.Set(x, y, color.RGBA{uint8(40 * i), uint8(x * 2), uint8(y * 3), 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, m, &jpeg.Options{Quality: 90})
	return b.Bytes()
}
