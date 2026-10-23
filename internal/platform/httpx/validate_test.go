package httpx

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

const thingsSpec = `
openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /things:
    post:
      operationId: createThing
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              required: [name]
              properties:
                name: {type: string, minLength: 2}
      responses:
        "201": {description: created}
`

// thingsServer validates against thingsSpec and echoes the decoded name.
func thingsServer(t *testing.T) http.Handler {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData([]byte(thingsSpec))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	validate, err := ValidateRequests(doc, log)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter()
	r.HandleFunc("POST /things", func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if err := Bind(req, &in); err != nil {
			WriteError(w, req, log, err)
			return
		}
		Respond(w, http.StatusCreated, in)
	})
	return Trace(validate(r))
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/things", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return serve(h, req)
}

func TestValidBodyReachesHandler(t *testing.T) {
	t.Parallel()
	rec := post(t, thingsServer(t), `{"name":"ab"}`)
	if rec.Code != 201 || rec.Body.String() != `{"name":"ab"}`+"\n" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestShortNameIsInvalidRequest(t *testing.T) {
	t.Parallel()
	rec := post(t, thingsServer(t), `{"name":"a"}`)
	b := decodeError(t, rec)
	if rec.Code != 400 || b.Error.Code != "invalid_request" || b.Error.Fields["name"] != "minLength" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestUnknownFieldIsInvalidRequest(t *testing.T) {
	t.Parallel()
	rec := post(t, thingsServer(t), `{"name":"ab","extra":1}`)
	b := decodeError(t, rec)
	if rec.Code != 400 || b.Error.Code != "invalid_request" || b.Error.Fields["extra"] != "additionalProperties" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestMalformedJSON(t *testing.T) {
	t.Parallel()
	rec := post(t, thingsServer(t), `{`)
	if b := decodeError(t, rec); rec.Code != 400 || b.Error.Code != "malformed_json" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestEmptyBodyIsInvalidRequest(t *testing.T) {
	t.Parallel()
	rec := post(t, thingsServer(t), ``)
	if b := decodeError(t, rec); rec.Code != 400 || b.Error.Code != "invalid_request" || b.Error.Fields["body"] != "required" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestBodyOverOneMegabyte(t *testing.T) {
	t.Parallel()
	big := `{"name":"` + strings.Repeat("a", 1<<20) + `"}`
	rec := post(t, thingsServer(t), big)
	if b := decodeError(t, rec); rec.Code != 413 || b.Error.Code != "body_too_large" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestPathOutsideSpecPassesThrough(t *testing.T) {
	t.Parallel()
	rec := serve(thingsServer(t), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/elsewhere", http.NoBody))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != 404 {
		t.Fatalf("%d %s", rec.Code, body)
	}
}

func TestValidationNeverLogsTheBody(t *testing.T) {
	t.Parallel()
	doc, _ := openapi3.NewLoader().LoadFromData([]byte(thingsSpec))
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	validate, _ := ValidateRequests(doc, log)
	h := Trace(validate(NewRouter()))

	rec := post(t, h, `{"name":"ab","extra":"hunter2-otp-583920"}`)
	if rec.Code != 400 {
		t.Fatalf("%d", rec.Code)
	}
	if strings.Contains(logs.String(), "hunter2") || strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("request value leaked\nlog: %s\nbody: %s", logs.String(), rec.Body.String())
	}
}

const formatsSpec = `
openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /items/{id}:
    get:
      operationId: getItem
      parameters:
        - {name: id, in: path, required: true, schema: {type: string, format: uuid}}
      responses:
        "200": {description: ok}
  /contacts:
    post:
      operationId: addContact
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                email: {type: string, format: email}
      responses:
        "201": {description: created}
`

func TestFormatsAreValidated(t *testing.T) {
	t.Parallel()
	doc, err := openapi3.NewLoader().LoadFromData([]byte(formatsSpec))
	if err != nil {
		t.Fatal(err)
	}
	validate, _ := ValidateRequests(doc, slog.New(slog.DiscardHandler))
	r := NewRouter()
	r.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.HandleFunc("POST /contacts", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	h := Trace(validate(r))

	if rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/not-a-uuid", http.NoBody)); rec.Code != 400 {
		t.Errorf("bad uuid: %d", rec.Code)
	}
	if rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/01928c3e-7b5a-7c1e-8a2b-3c4d5e6f7a8b", http.NoBody)); rec.Code != 200 {
		t.Errorf("good uuid: %d", rec.Code)
	}
	for body, want := range map[string]int{`{"email":"not-an-email"}`: 400, `{"email":"linh@example.com"}`: 201} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/contacts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if rec := serve(h, req); rec.Code != want {
			t.Errorf("%s: %d, want %d", body, rec.Code, want)
		}
	}
}
