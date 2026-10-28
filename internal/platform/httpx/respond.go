package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"go.opentelemetry.io/otel/trace"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// Respond writes body as JSON with status.
func Respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent; a failed write means the client left.
	_ = json.NewEncoder(w).Encode(body)
}

var statusOf = map[apperr.Kind]int{
	apperr.KindBadRequest:           http.StatusBadRequest,
	apperr.KindInvalid:              http.StatusUnprocessableEntity,
	apperr.KindNotFound:             http.StatusNotFound,
	apperr.KindConflict:             http.StatusConflict,
	apperr.KindUnauthenticated:      http.StatusUnauthorized,
	apperr.KindForbidden:            http.StatusForbidden,
	apperr.KindRateLimited:          http.StatusTooManyRequests,
	apperr.KindPreconditionRequired: http.StatusPreconditionRequired,
	apperr.KindTooLarge:             http.StatusRequestEntityTooLarge,
	apperr.KindUnavailable:          http.StatusServiceUnavailable,
}

type errorPayload struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Details map[string]any    `json:"details,omitempty"`
	TraceID string            `json:"trace_id"`
}

// WriteError writes err in the shared error format. An *apperr.Error anywhere
// in the chain decides the status; anything else is a 500 whose real cause
// goes to the log only, because raw errors reveal tables and constraints.
func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	traceID := trace.SpanContextFromContext(r.Context()).TraceID().String()

	var ae *apperr.Error
	status, ok := http.StatusInternalServerError, errors.As(err, &ae)
	if ok {
		status, ok = statusOf[ae.Kind]
	}
	if !ok {
		log.ErrorContext(r.Context(), "request failed", "err", err)
		writeInternal(w, traceID)
		return
	}
	if ae.Err != nil {
		log.WarnContext(r.Context(), "request refused", "code", ae.Code, "err", ae.Err)
	}
	Respond(w, status, map[string]errorPayload{"error": {
		Code: ae.Code, Message: ae.Message, Fields: ae.Fields, Details: ae.Details, TraceID: traceID,
	}})
}

func writeInternal(w http.ResponseWriter, traceID string) {
	Respond(w, http.StatusInternalServerError, map[string]errorPayload{"error": {
		Code: "internal", Message: "internal error", TraceID: traceID,
	}})
}

// Recover turns a panic into a 500 and logs the value with its stack.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler { //nolint:errorlint // v is the panic value; net/http compares it the same way.
					panic(v)
				}
				log.ErrorContext(r.Context(), "panic", "value", fmt.Sprint(v), "stack", string(debug.Stack()))
				writeInternal(w, trace.SpanContextFromContext(r.Context()).TraceID().String())
			}()
			next.ServeHTTP(w, r)
		})
	}
}
