package stream

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

// keepAlive is how often an idle stream writes a comment, so proxies and
// the app see it alive.
const keepAlive = 25 * time.Second

// Handler serves GET /v1/stream to a signed-in caller: an event ready, then
// one event per hint for the caller's account, until the caller leaves or
// the hub closes.
func (h *Hub) Handler(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		rc := http.NewResponseController(w)
		// The server's WriteTimeout would end the stream.
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			httpx.WriteError(w, r, log, fmt.Errorf("clear the write deadline: %w", err))
			return
		}
		hints, stop := h.Subscribe(p.AccountID)
		defer stop()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		send := func(event string, data []byte) bool {
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
				return false
			}
			return rc.Flush() == nil
		}
		if !send("ready", []byte("{}")) {
			return
		}
		tick := time.NewTicker(keepAlive)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case hint, open := <-hints:
				if !open {
					return
				}
				data, err := json.Marshal(hint)
				if err != nil || !send(hint.Kind, data) {
					return
				}
			case <-tick.C:
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil || rc.Flush() != nil {
					return
				}
			}
		}
	})
}
