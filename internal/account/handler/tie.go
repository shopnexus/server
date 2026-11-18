package handler

import (
	"context"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

// accountBody names the other account in the body: POST /v1/blocks and
// POST /v1/follows.
type accountBody struct {
	AccountID uuid.UUID `json:"account_id"`
}

func (b accountBody) account() uuid.UUID { return b.AccountID }

// accountPath names the other account in the path: DELETE
// /v1/blocks/{account_id} and DELETE /v1/follows/{account_id}.
type accountPath struct {
	AccountID uuid.UUID `json:"-" path:"account_id"`
}

func (p accountPath) account() uuid.UUID { return p.AccountID }

// tie serves an endpoint that sets or lifts one of the caller's ties to
// another account, a block or a follow: it binds T, calls set with the
// caller and the account, and answers 204.
func tie[T interface {
	accountBody | accountPath
	account() uuid.UUID
}](h *Handler, set func(ctx context.Context, caller, other uuid.UUID) error,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, h.log)
		if !ok {
			return
		}
		var in T
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
		if err := set(r.Context(), p.AccountID, in.account()); err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ties serves a page of the caller's ties of one kind, newest first:
// GET /v1/blocks and GET /v1/follows.
func ties[V any](h *Handler, list func(ctx context.Context, caller uuid.UUID, p page.Request) (page.Page[V], error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, h.log)
		if !ok {
			return
		}
		var in page.Request
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
		out, err := list(r.Context(), p.AccountID, in)
		if err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, out)
	}
}
