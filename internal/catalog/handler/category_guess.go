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
	httpx.Serve(w, r, h.log, http.StatusOK, func(in categoryGuessRequest) (categoryGuess, error) {
		code, err := h.svc.GuessCategory(r.Context(), in.Title, in.Description)
		out := categoryGuess{}
		if code != "" {
			out.CategoryCode = &code
		}
		return out, err
	})
}
