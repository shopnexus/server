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

// The model's JSON replies; the iPhone's description keeps a phone for the
// regex to find.
const (
	bicycleText = `{"subject":"xe đạp","found":true,"title":"Xe đạp Asama xanh",` +
		`"description":"Xe đi ít, có thương lượng.","reason":""}`
	iphoneLinkText = `{"subject":"điện thoại","found":true,"title":"iPhone 11 64GB Trắng, pin 81%",` +
		`"description":"Máy dùng 3 năm, zin all. Liên hệ 0909 123 456.","reason":""}`
	bicyclePhotoText = `{"subject":"xe đạp","found":true,"title":"Xe đạp Asama xanh",` +
		`"description":"Xe đạp Asama màu xanh, có giỏ trước.","reason":""}`
	noItemText = `{"subject":"căn phòng trống","found":false,"title":"","description":"",` +
		`"reason":"Video chỉ có một căn phòng trống."}`
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
				Stream   bool `json:"stream"`
				Messages []struct {
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Messages) != 2 || !req.Stream {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			var text string
			if json.Unmarshal(req.Messages[1].Content, &text) == nil {
				// A link read sends text only.
				answer := iphoneLinkText
				if strings.Contains(text, "căn hộ") {
					answer = noItemText
				}
				writeStubStream(w, answer)
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
			answer := bicycleText
			for _, p := range parts {
				switch {
				case p.Type == "text" && strings.HasPrefix(p.Text, "The seller's photo"):
					answer = bicyclePhotoText
				case p.Type == "text" && strings.Contains(p.Text, "không có gì"):
					answer = noItemText
				default:
				}
			}
			writeStubStream(w, answer)
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

// writeStubStream streams text as chat completion chunks, a few runes
// each.
func writeStubStream(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	runes := []rune(text)
	for i := 0; i < len(runes); i += 8 {
		chunk, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": string(runes[i:min(i+8, len(runes))])}}}})
		_, _ = w.Write([]byte("data: " + string(chunk) + "\n\n"))
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
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
