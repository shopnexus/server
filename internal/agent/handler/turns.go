package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type eventsRequest struct {
	ID          uuid.UUID `json:"-"               path:"id"`
	LastEventID string    `header:"Last-Event-Id" json:"-"`
	// After is Last-Event-ID for a page that opens the stream again, since
	// EventSource sends the header only on its own reconnects.
	After string `json:"-" query:"after"`
}

// Events serves GET /v1/console/agent/turns/{id}/events: the turn's
// events as the service emits them, each flushed, with no write deadline.
// The stream ends after turn.finished, turn.paused or clarify; the member
// closing the tab ends it, and the run goes on.
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in eventsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	last := in.LastEventID
	if last == "" {
		last = in.After
	}
	rc := http.NewResponseController(w)
	started := false
	emit := func(e service.TurnEvent) error {
		if !started {
			started = true
			// The server's write timeout is for answers; a stream lasts the run.
			if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
				h.log.WarnContext(r.Context(), "agent: lift the write deadline of an event stream", "err", err)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
		}
		var err error
		switch {
		case e.Retry > 0:
			_, err = fmt.Fprintf(w, "retry: %d\n\n", e.Retry.Milliseconds())
		case e.KeepAlive:
			_, err = io.WriteString(w, ": keep-alive\n\n")
		default:
			_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, e.Data)
		}
		if err != nil {
			return fmt.Errorf("write event: %w", err)
		}
		return rc.Flush()
	}
	if err := h.svc.Events(r.Context(), caller, in.ID, last, emit); err != nil && !started {
		httpx.WriteError(w, r, h.log, err)
	}
}

// ConsoleAgents serves GET /v1/console/agents.
func (h *Handler) ConsoleAgents(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, _ httpx.None) (items[service.ConsoleAgent], error) {
		return listOf(h.svc.ConsoleAgents(r.Context()))
	})
}

// GetOrder serves GET /v1/console/orders/{id}.
func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in idRequest) (service.ConsoleOrder, error) {
		return h.svc.Order(r.Context(), c, in.ID)
	})
}

// GetListing serves GET /v1/console/listings/{id}.
func (h *Handler) GetListing(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in idRequest) (service.ConsoleListing, error) {
		return h.svc.Listing(r.Context(), c, in.ID)
	})
}
