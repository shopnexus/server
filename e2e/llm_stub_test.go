package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// stubModel stands in for the search model: it answers from a fixed map by
// the query and counts the calls per query.
type stubModel struct {
	srv   *httptest.Server
	mu    sync.Mutex
	count map[string]int
}

// answers are what the model says of a query; any other query gets nothing.
var answers = map[string]string{
	"xe dap di lam duoi 3tr": `{"understood":"Xe đạp đi làm, dưới 3 triệu","boosts":[` +
		`{"attr":"phrase","values":["xe đạp đi làm"]},{"attr":"category","values":["vehicles.bicycle"]},` +
		`{"attr":"price","values":["-3000000"]}],"demotes":[]}`,
}

func newModel() *stubModel {
	m := &stubModel{count: map[string]int{}}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if r.URL.Path != "/chat/completions" || json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Messages) != 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		q := req.Messages[1].Content
		m.mu.Lock()
		m.count[q]++
		m.mu.Unlock()
		answer, ok := answers[q]
		if !ok {
			answer = `{"understood":"","boosts":[],"demotes":[]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": answer}}},
		})
	}))
	return m
}

func (m *stubModel) url() string { return m.srv.URL }

// asked reports how often the model read the stub's one understood query.
func (m *stubModel) asked() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.count["xe dap di lam duoi 3tr"]
}
