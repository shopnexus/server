package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shopnexus/server/internal/assist/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// writeWait is how long one event may take to write; the server's write
// timeout is shorter than a watch.
const writeWait = 10 * time.Second

// ReadEvents serves GET /v1/listing-reads/{id}/events, a server-sent event
// stream. An error before the first event is a plain error answer.
func (h *Handler) ReadEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in readRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	rc := http.NewResponseController(w)
	started := false
	emit := func(e service.ReadEvent) error {
		if !started {
			started = true
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
		}
		if err := rc.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			return fmt.Errorf("set write deadline: %w", err)
		}
		var err error
		switch {
		case e.Ping:
			_, err = fmt.Fprint(w, ": ping\n\n")
		case e.Text != nil:
			err = writeEvent(w, "text", e.Text)
		default:
			err = writeEvent(w, "read", e.Read)
		}
		if err != nil {
			return err
		}
		return rc.Flush()
	}
	err := h.svc.WatchRead(r.Context(), p.AccountID, in.ID, emit)
	if err != nil && !started {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err != nil && r.Context().Err() == nil {
		h.log.WarnContext(r.Context(), "read events ended early", "read", in.ID, "err", err)
	}
}

func writeEvent(w http.ResponseWriter, name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
	return err
}
