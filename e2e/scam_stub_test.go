package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
)

// scamWords are the words the scam-check stub answers a code for.
var scamWords = map[string]string{"cọc": "prepayment_request", "zalo": "off_app_contact", "otp": "sensitive_info_request"}

// stubScam stands in for the scam-check model: each side's codes come from
// the words in its lines, "A: text" or "B: text".
type stubScam struct{ srv *httptest.Server }

func newScam() *stubScam {
	s := &stubScam{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if r.URL.Path != "/chat/completions" || json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Messages) != 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		codes := map[string][]string{"a": {}, "b": {}}
		for line := range strings.SplitSeq(req.Messages[1].Content, "\n") {
			side, text, ok := strings.Cut(line, ": ")
			if !ok {
				continue
			}
			side = strings.ToLower(side)
			for word, code := range scamWords {
				if strings.Contains(strings.ToLower(text), word) && !slices.Contains(codes[side], code) {
					codes[side] = append(codes[side], code)
				}
			}
		}
		answer, _ := json.Marshal(codes)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": string(answer)}}},
		})
	}))
	return s
}

func (s *stubScam) url() string { return s.srv.URL }
