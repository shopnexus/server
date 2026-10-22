package httpx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// defineFormats registers the string formats the spec relies on as shape
// rules; kin-openapi validates only a few by default. The registry is global
// to kin-openapi, so it runs once.
var defineFormats = sync.OnceFunc(func() {
	openapi3.DefineStringFormatValidator("uuid", openapi3.NewRegexpFormatValidator(openapi3.FormatOfStringForUUIDOfRFC9562)) // v1-v8; ids are v7
	openapi3.DefineStringFormatValidator("email", openapi3.NewRegexpFormatValidator(openapi3.FormatOfStringForEmail))
})

// MaxBodyBytes caps every request body.
const MaxBodyBytes = 1 << 20

var (
	errInvalidRequest = apperr.New(apperr.KindBadRequest, "invalid_request", "request does not match the API description")
	errBodyTooLarge   = apperr.New(apperr.KindTooLarge, "body_too_large", "request body exceeds 1 MB")
)

// ValidateRequests checks each request against its operation in doc before
// the handler runs. Shape rules live only in the spec. Requests to paths the
// spec does not describe pass through, so the mux answers 404.
func ValidateRequests(doc *openapi3.T, log *slog.Logger) (func(http.Handler) http.Handler, error) {
	defineFormats()
	// Match on paths only; the servers list names deployments, not this process.
	matchDoc := *doc
	matchDoc.Servers = nil
	router, err := legacy.NewRouter(&matchDoc)
	if err != nil {
		return nil, fmt.Errorf("build openapi router: %w", err)
	}
	opts := &openapi3filter.Options{
		MultiError:         true,
		AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, params, err := router.FindRoute(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			if r.Body != nil {
				body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
				if err != nil {
					if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
						WriteError(w, r, log, errBodyTooLarge)
						return
					}
					WriteError(w, r, log, err)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
			}

			in := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route, Options: opts}
			if err := openapi3filter.ValidateRequest(r.Context(), in); err != nil {
				WriteError(w, r, log, toAppError(err))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// toAppError turns kin-openapi's validation errors into one invalid_request
// error with a field entry per problem, or malformed_json for a body that
// does not parse. A field's problem is the schema keyword it broke
// (minLength, required, additionalProperties, ...).
//
// The kin-openapi error is not kept as the cause: it embeds the request value,
// which may be a password or an OTP code, and causes are logged.
func toAppError(err error) error {
	out := *errInvalidRequest
	add := func(name, problem string) {
		out = *out.WithField(name, problem)
	}

	var all []error
	if me := (openapi3.MultiError{}); errors.As(err, &me) {
		all = me
	} else {
		all = []error{err}
	}
	for _, e := range all {
		var reqErr *openapi3filter.RequestError
		if !errors.As(e, &reqErr) {
			add("request", "invalid")
			continue
		}
		if _, ok := errors.AsType[*openapi3filter.ParseError](reqErr.Err); ok {
			return ErrMalformedJSON
		}
		field := "body"
		if reqErr.Parameter != nil {
			field = reqErr.Parameter.Name
		}
		var schemaErrs []error
		if me := (openapi3.MultiError{}); errors.As(reqErr.Err, &me) {
			schemaErrs = me
		} else {
			schemaErrs = []error{reqErr.Err}
		}
		for _, se := range schemaErrs {
			var schemaErr *openapi3.SchemaError
			if !errors.As(se, &schemaErr) {
				// A missing required body or parameter.
				add(field, "required")
				continue
			}
			name := field
			if p := schemaErr.JSONPointer(); len(p) > 0 {
				name = strings.Join(p, ".")
			}
			if schemaErr.SchemaField == "additionalProperties" {
				name = quotedName(schemaErr.Reason, name)
			}
			add(name, schemaErr.SchemaField)
		}
	}
	return &out
}

// quotedName pulls the property out of kin-openapi's
// "additional properties 'extra' not allowed".
func quotedName(reason, fallback string) string {
	_, rest, ok := strings.Cut(reason, "properties '")
	if !ok {
		return fallback
	}
	name, _, ok := strings.Cut(rest, "'")
	if !ok || name == "" {
		return fallback
	}
	return name
}
