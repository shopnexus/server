package stream_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/stream"
)

// The handler logs only the kinds the log holds, even for a body the
// spec's check never saw.
func TestDevUpdateHandlerLogsOnlyLoggedKinds(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	updates := stream.PGLog{DB: db}
	h := stream.DevUpdateHandler(updates, slog.New(slog.DiscardHandler))
	me := uuid.NewV7()
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/dev/updates", strings.NewReader(body))
		req = req.WithContext(authn.With(req.Context(), authn.Principal{AccountID: me, SessionID: uuid.NewV7(), AuthTime: time.Now()}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, kind := range []string{"typing", "video_read", ""} {
		if rec := post(`{"kind":"` + kind + `"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("kind %q: %d %s", kind, rec.Code, rec.Body)
		}
	}
	if seq, err := updates.Current(t.Context(), me); err != nil || seq != 0 {
		t.Fatalf("seq %d after refused kinds: %v", seq, err)
	}
	if rec := post(`{"kind":"deal","deal_id":"` + uuid.NewV7().String() + `"}`); rec.Code != http.StatusCreated ||
		!strings.Contains(rec.Body.String(), `"seq":1`) {
		t.Fatalf("a deal: %d %s", rec.Code, rec.Body)
	}
}
