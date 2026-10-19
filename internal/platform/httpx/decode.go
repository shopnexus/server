package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// ErrMalformedJSON is a body that does not parse as the expected JSON.
var ErrMalformedJSON = apperr.New(apperr.KindBadRequest, "malformed_json", "request body is not valid JSON")

// Decode reads the JSON body into a T. ValidateRequests has already checked
// the body against the spec; unknown fields are rejected here as well so a
// handler outside the spec cannot accept them silently.
func Decode[T any](r *http.Request) (T, error) {
	var v T
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		e := *ErrMalformedJSON
		e.Err = err
		return v, &e
	}
	return v, nil
}
