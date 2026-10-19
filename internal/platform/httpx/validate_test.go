package httpx

import (
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
	useRecorder(t)
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
		in, err := Decode[struct {
			Name string `json:"name"`
		}](req)
		if err != nil {
			WriteError(w, req, log, err)
			return
		}
		Respond(w, req, http.StatusCreated, in)
	})
	return Trace(validate(r))
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/things", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return serve(h, req)
}

func TestValidBodyReachesHandler(t *testing.T) {
	rec := post(t, thingsServer(t), `{"name":"ab"}`)
	if rec.Code != 201 || rec.Body.String() != `{"name":"ab"}`+"\n" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestShortNameIsInvalidRequest(t *testing.T) {
	rec := post(t, thingsServer(t), `{"name":"a"}`)
	b := decodeError(t, rec)
	if rec.Code != 400 || b.Error.Code != "invalid_request" || b.Error.Fields["name"] != "minLength" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestUnknownFieldIsInvalidRequest(t *testing.T) {
	rec := post(t, thingsServer(t), `{"name":"ab","extra":1}`)
	b := decodeError(t, rec)
	if rec.Code != 400 || b.Error.Code != "invalid_request" || b.Error.Fields["extra"] != "additionalProperties" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestMalformedJSON(t *testing.T) {
	rec := post(t, thingsServer(t), `{`)
	if b := decodeError(t, rec); rec.Code != 400 || b.Error.Code != "malformed_json" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestEmptyBodyIsInvalidRequest(t *testing.T) {
	rec := post(t, thingsServer(t), ``)
	if b := decodeError(t, rec); rec.Code != 400 || b.Error.Code != "invalid_request" || b.Error.Fields["body"] != "required" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestBodyOverOneMegabyte(t *testing.T) {
	big := `{"name":"` + strings.Repeat("a", 1<<20) + `"}`
	rec := post(t, thingsServer(t), big)
	if b := decodeError(t, rec); rec.Code != 413 || b.Error.Code != "body_too_large" {
		t.Fatalf("%d %+v", rec.Code, b)
	}
}

func TestPathOutsideSpecPassesThrough(t *testing.T) {
	rec := serve(thingsServer(t), httptest.NewRequest("GET", "/elsewhere", nil))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != 404 {
		t.Fatalf("%d %s", rec.Code, body)
	}
}
