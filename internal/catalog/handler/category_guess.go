package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/httpx"
)

// categoryGuessRequest is CategoryGuessRequest; the request validator holds
// the shape rules.
type categoryGuessRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// categoryGuess is CategoryGuess.
type categoryGuess struct {
	CategoryCode *string `json:"category_code"`
}

// GuessCategory serves POST /v1/category-guesses.
func (h *Handler) GuessCategory(w http.ResponseWriter, r *http.Request) {
	var in categoryGuessRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	code, err := h.svc.GuessCategory(r.Context(), in.Title, in.Description)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out := categoryGuess{}
	if code != "" {
		out.CategoryCode = &code
	}
	httpx.Respond(w, http.StatusOK, out)
}
