package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/telemetry"
)

type errorBody struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
		TraceID string            `json:"trace_id"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var b errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	if b.Error.TraceID == "" || b.Error.TraceID != rec.Header().Get("Trace-Id") {
		t.Fatalf("trace_id %q, header %q", b.Error.TraceID, rec.Header().Get("Trace-Id"))
	}
	return b
}

// failWith serves err through WriteError behind the trace middleware.
func failWith(t *testing.T, err error) (*httptest.ResponseRecorder, *bytes.Buffer) {
	t.Helper()
	useRecorder(t)
	var logs bytes.Buffer
	log := telemetry.NewLogger(&logs, "json", slog.LevelInfo)
	h := Trace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, log, err)
	}))
	return serve(h, httptest.NewRequest("POST", "/x", nil)), &logs
}

func TestWriteErrorMapsKinds(t *testing.T) {
	cases := []struct {
		kind   apperr.Kind
		status int
	}{
		{apperr.KindBadRequest, 400},
		{apperr.KindUnauthenticated, 401},
		{apperr.KindForbidden, 403},
		{apperr.KindNotFound, 404},
		{apperr.KindConflict, 409},
		{apperr.KindInvalid, 422},
		{apperr.KindPreconditionRequired, 428},
		{apperr.KindRateLimited, 429},
	}
	for _, c := range cases {
		rec, _ := failWith(t, apperr.New(c.kind, "some_code", "some message"))
		if rec.Code != c.status {
			t.Errorf("kind %v: status %d, want %d", c.kind, rec.Code, c.status)
		}
		if b := decodeError(t, rec); b.Error.Code != "some_code" || b.Error.Message != "some message" {
			t.Errorf("kind %v: body %+v", c.kind, b)
		}
	}
}

func TestWriteErrorUnwrapsAppError(t *testing.T) {
	rec, _ := failWith(t, fmt.Errorf("place order: %w", apperr.New(apperr.KindConflict, "phone_taken", "taken").WithField("phone", "taken")))
	b := decodeError(t, rec)
	if rec.Code != 409 || b.Error.Code != "phone_taken" || b.Error.Fields["phone"] != "taken" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestWriteErrorHidesUnexpectedErrors(t *testing.T) {
	secret := `ERROR: duplicate key value violates unique constraint "identities_kind_value_key"`
	rec, logs := failWith(t, fmt.Errorf("db: %w", errors.New(secret)))

	b := decodeError(t, rec)
	if rec.Code != 500 || b.Error.Code != "internal" || b.Error.Message != "internal error" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
	if strings.Contains(rec.Body.String(), "identities") {
		t.Fatalf("body leaks the database error: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "identities_kind_value_key") || !strings.Contains(logs.String(), b.Error.TraceID) {
		t.Fatalf("log lacks the real error or trace id: %s", logs.String())
	}
}

func TestRespondWritesJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	Respond(rec, httptest.NewRequest("GET", "/", nil), http.StatusCreated, map[string]int{"n": 1})
	if rec.Code != 201 || rec.Header().Get("Content-Type") != "application/json" || rec.Body.String() != `{"n":1}`+"\n" {
		t.Fatalf("%d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestRecoverTurnsPanicInto500(t *testing.T) {
	useRecorder(t)
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	h := Trace(Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	})))
	rec := serve(h, httptest.NewRequest("GET", "/", nil))

	if b := decodeError(t, rec); rec.Code != 500 || b.Error.Code != "internal" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
	if !strings.Contains(logs.String(), "kaboom") || !strings.Contains(logs.String(), "goroutine") {
		t.Fatalf("log lacks panic value or stack: %s", logs.String())
	}
}

func TestRouterRecordsRoutes(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("GET /a/{id}", func(http.ResponseWriter, *http.Request) {})
	r.HandleFunc("POST /b", func(http.ResponseWriter, *http.Request) {})
	got := r.Routes()
	if len(got) != 2 || got[0] != (Route{"GET", "/a/{id}"}) || got[1] != (Route{"POST", "/b"}) {
		t.Fatalf("routes %v", got)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/b", nil))
	if rec.Code != 200 {
		t.Fatalf("dispatch %d", rec.Code)
	}
}
