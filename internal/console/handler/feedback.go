package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

// ListFeedback serves GET /v1/console/feedback.
func (h *Handler) ListFeedback(w http.ResponseWriter, r *http.Request) {
	var in page.Request
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.feedback.Feedback(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}
