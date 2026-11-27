package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// turnTokenPrefix marks a turn token among bearer tokens.
const turnTokenPrefix = "snt_"

// Gate lets an agent's turn call the console API with its token, for the
// member it works for (UC-MOD-08). A request with a turn token passes only
// to an operation its agent's capabilities grant and the member's role
// may call, route naming the route pattern that serves it; the token is its only credential. A change gets
// the approval it would wait on as its Idempotency-Key, so its replay is
// that one request. Every call is recorded with its status.
//
// The gate sits outside the request validator, so a change is checked
// against the spec before the console's guard holds it.
func (h *Handler) Gate(route func(method, path string) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || !strings.HasPrefix(token, turnTokenPrefix) {
				next.ServeHTTP(w, r)
				return
			}
			turn, err := h.svc.TurnOfToken(r.Context(), token)
			if err != nil {
				httpx.WriteError(w, r, h.log, err)
				return
			}
			op, ok := h.svc.Operation(r.Method, route(r.Method, r.URL.Path))
			if !ok || !domain.Grants(turn.Capabilities, turn.Role, op) {
				httpx.WriteError(w, r, h.log, domain.ErrOperationForbidden)
				return
			}
			r.Header.Del("Authorization")
			r.Header.Del("Cookie")
			approval := uuid.NewV7()
			if op.Approval && r.Header.Get("Idempotency-Key") == "" {
				r.Header.Set("Idempotency-Key", approval.String())
			}
			turn.Hold = func(w http.ResponseWriter, r *http.Request) { h.hold(w, r, turn, op, approval) }
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(sw, r.WithContext(authn.WithTurn(r.Context(), turn)))
			h.svc.RecordCall(r.Context(), turn, op, r.Method, r.URL.RequestURI(), sw.status, time.Since(start))
		})
	}
}

// statusWriter notes the status a handler wrote.
type statusWriter struct {
	http.ResponseWriter

	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// replayHeaders are the request headers an approval keeps to replay with.
var replayHeaders = []string{"Content-Type", "Idempotency-Key", "If-Match"}

// held is the answer to a change an agent asked for, which only the model
// reads: the approval it now waits on. The rest of the approval is what the
// model just sent, and the harness says what an approval is.
type held struct {
	ApprovalID uuid.UUID `json:"approval_id"`
	Status     string    `json:"status"`
}

// hold answers a change of an agent's turn, which the console's guard
// let through as the member's, with 202 and the approval it waits on.
func (h *Handler) hold(w http.ResponseWriter, r *http.Request, turn authn.Turn, op domain.Operation, id uuid.UUID) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	req := service.Request{ID: id, Method: r.Method, Path: r.URL.RequestURI(), Headers: map[string]string{}}
	if len(bytes.TrimSpace(body)) > 0 {
		if !json.Valid(body) {
			httpx.WriteError(w, r, h.log, httpx.ErrMalformedJSON)
			return
		}
		req.Body = body
	}
	for _, k := range replayHeaders {
		if v := r.Header.Get(k); v != "" {
			req.Headers[k] = v
		}
	}
	a, err := h.svc.Hold(r.Context(), turn, op, req)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusAccepted, held{ApprovalID: a.ID, Status: a.Status})
}

type operationList struct {
	Operations []domain.Operation `json:"operations"`
}

// turnOf is the agent's turn a request comes from, or nil for a member.
func turnOf(r *http.Request) *authn.Turn {
	if t, ok := authn.TurnFrom(r.Context()); ok {
		return &t
	}
	return nil
}

// Operations serves GET /v1/console/agent/operations.
func (h *Handler) Operations(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, _ httpx.None) (operationList, error) {
		ops, err := h.svc.Operations(r.Context(), c, turnOf(r))
		return operationList{ops}, err
	})
}

type approvalsRequest struct {
	ConversationID *uuid.UUID `json:"-" query:"conversation_id"`
	Status         string     `json:"-" query:"status"`
}

// Approvals serves GET /v1/console/agent/approvals.
func (h *Handler) Approvals(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in approvalsRequest) (items[service.Approval], error) {
		return listOf(h.svc.Approvals(r.Context(), c, turnOf(r), service.ApprovalQuery(in)))
	})
}

// Approve serves POST /v1/console/agent/approvals/{id}/approve.
func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in idRequest) (service.Approval, error) {
		return h.svc.Approve(r.Context(), c, in.ID, h.replay(r))
	})
}

// Reject serves POST /v1/console/agent/approvals/{id}/reject.
func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in idRequest) (service.Approval, error) {
		return h.svc.Reject(r.Context(), c, in.ID)
	})
}

// replayTimeout bounds an approved request.
const replayTimeout = time.Minute

// sameCaller are the headers of the approver's request an approved
// request carries, so it is theirs: their session, the console's header,
// and where it came from.
var sameCaller = []string{"Cookie", "X-Requested-With", "User-Agent", "X-Forwarded-For", "X-Real-Ip"}

var errNoRoot = errors.New("agent: the server's handler is not set")

// replay makes an approved request through the server's whole handler,
// as from made it: with its session, so the approver's rights apply and
// the audit log names them.
func (h *Handler) replay(from *http.Request) service.Replay {
	return func(req service.Request) (int, []byte, error) {
		root := h.root.Load()
		if root == nil {
			return 0, nil, errNoRoot
		}
		ctx, cancel := context.WithTimeout(context.Background(), replayTimeout)
		defer cancel()
		r, err := http.NewRequestWithContext(ctx, req.Method, req.Path, bytes.NewReader(req.Body))
		if err != nil {
			return 0, nil, err
		}
		r.Host, r.RemoteAddr = from.Host, from.RemoteAddr
		for k, v := range req.Headers {
			r.Header.Set(k, v)
		}
		for _, k := range sameCaller {
			if v := from.Header.Values(k); len(v) > 0 {
				r.Header[http.CanonicalHeaderKey(k)] = v
			}
		}
		rec := &recorder{header: http.Header{}, status: http.StatusOK}
		(*root).ServeHTTP(rec, r)
		return rec.status, rec.body.Bytes(), nil
	}
}

// recorder keeps what a handler answered.
type recorder struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (w *recorder) Header() http.Header { return w.header }

func (w *recorder) WriteHeader(status int) {
	if !w.wrote {
		w.status, w.wrote = status, true
	}
}

func (w *recorder) Write(b []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body.Write(b)
}
