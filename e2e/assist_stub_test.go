package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	bicycleAnswer = `{"is_item":true,"title":"Xe đạp Asama xanh","category_code":"vehicles.bicycle","condition":"used",` +
		`"specs":[{"label":"Màu","value":"Xanh"}],"description":"Xe đi ít, giá một triệu hai, có thương lượng.",` +
		`"spoken_price":1200000,"negotiable":true,"kept":[2,0],` +
		`"estimate":{"low":1000000,"median":1200000,"high":1400000}}`
	iphoneLinkAnswer = `{"is_item":true,"title":"iPhone 11 64GB Trắng, pin 81%","category_code":"phones.iphone","condition":"used",` +
		`"specs":[{"label":"Dung lượng","value":"64 GB"}],"description":"Máy dùng 3 năm, zin all. Có thương lượng.",` +
		`"negotiable":true,"contact_removed":true,"estimate":{"low":4000000,"median":4500000,"high":5000000}}`
	flatLinkAnswer = `{"is_item":false,"title":"","category_code":"","condition":"used","specs":[],"description":"",` +
		`"negotiable":false,"contact_removed":false,"estimate":null}`
	noItemAnswer = `{"is_item":false,"title":"","category_code":"","condition":"used","specs":[],"description":"",` +
		`"spoken_price":null,"negotiable":false,"kept":[],"estimate":null}`
)

// stubAssist stands in for what a video read calls: ai's frames endpoint,
// the stt alias and the video-read model, on one server.
type stubAssist struct{ srv *httptest.Server }

func newAssist() *stubAssist {
	a := &stubAssist{}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/video/frames"):
			video, ok := fileOf(w, r)
			if !ok {
				return
			}
			if video == shortVideo {
				writeStubError(w, http.StatusBadRequest, "too_short")
				return
			}
			frames := make([]map[string]any, 3)
			for i := range frames {
				frames[i] = map[string]any{
					"at_seconds": float64(i) * 4, "sharpness": 100 + i,
					"jpeg": base64.StdEncoding.EncodeToString(frameJPEG(i)),
				}
			}
			writeStubJSON(w, map[string]any{"duration": 12.0, "frames": frames})
		case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
			video, ok := fileOf(w, r)
			if !ok {
				return
			}
			text := "mình bán xe đạp asama giá một triệu hai có thương lượng"
			if video == emptyRoom {
				text = "trong phòng không có gì"
			}
			writeStubJSON(w, map[string]any{"text": text, "duration": 12.0, "usage": map[string]any{"type": "duration", "seconds": 12}})
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

// fileOf reads the multipart field file.
func fileOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file", http.StatusBadRequest)
		return "", false
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read file", http.StatusBadRequest)
		return "", false
	}
	return string(b), true
}

func writeStubJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeStubError answers in OpenAI's error shape, as ai does.
func writeStubError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": code, "type": "invalid_request_error", "code": code},
	})
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
