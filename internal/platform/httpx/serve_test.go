package httpx

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
)

type serveIn struct {
	ID   uuid.UUID `json:"-"    path:"id"`
	Name string    `json:"name"`
}

type serveOut struct {
	Echo string `json:"echo"`
}

// ServeCaller refuses a request without a caller before it binds, answers
// the use case's value with the status, and maps its error.
func TestServeCaller(t *testing.T) {
	t.Parallel()
	account := uuid.NewV7()
	run := func(status int, signed bool, fail error) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /x/{id}", func(w http.ResponseWriter, r *http.Request) {
			ServeCaller(w, r, slog.New(slog.DiscardHandler), status, func(p authn.Principal, in serveIn) (serveOut, error) {
				if p.AccountID != account || in.Name != "lan" {
					t.Errorf("the use case got %v %+v", p.AccountID, in)
				}
				return serveOut{Echo: in.Name}, fail
			})
		})
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/"+uuid.NewV7().String(), strings.NewReader(`{"name":"lan"}`))
		if signed {
			req = req.WithContext(authn.With(req.Context(), authn.Principal{AccountID: account}))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := run(http.StatusCreated, true, nil); rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"echo":"lan"`) {
		t.Errorf("created: %d %s", rec.Code, rec.Body)
	}
	if rec := run(http.StatusNoContent, true, nil); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("no content: %d %q", rec.Code, rec.Body)
	}
	if rec := run(http.StatusOK, false, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", rec.Code)
	}
	if rec := run(http.StatusOK, true, apperr.New(apperr.KindNotFound, "x_missing", "gone")); rec.Code != http.StatusNotFound {
		t.Errorf("a failure: %d", rec.Code)
	}
}
