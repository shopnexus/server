package httpx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// defineFormats registers the string formats the spec relies on as shape
// rules; kin-openapi validates only a few by default. The registry is global
// to kin-openapi, so it runs once.
var defineFormats = sync.OnceFunc(func() {
	openapi3.DefineStringFormatValidator("uuid", openapi3.NewRegexpFormatValidator(openapi3.FormatOfStringForUUIDOfRFC9562)) // v1-v8; ids are v7
	openapi3.DefineStringFormatValidator("email", openapi3.NewRegexpFormatValidator(openapi3.FormatOfStringForEmail))
})

// maxBodyBytes caps every request body.
const maxBodyBytes = 1 << 20

var (
	errInvalidRequest = apperr.New(apperr.KindBadRequest, "invalid_request", "request does not match the API description")
	errBodyTooLarge   = apperr.New(apperr.KindTooLarge, "body_too_large", "request body exceeds 1 MB")
)

// ValidateRequests checks each request against its operation in doc before
// the handler runs. Shape rules live only in the spec. It matches only the
// operations in served, the routes the mux has: a request for any other,
// such as a dev route a production server does not mount, passes through
// unread, and the mux answers it as an unknown path.
func ValidateRequests(doc *openapi3.T, served []Route, log *slog.Logger) (func(http.Handler) http.Handler, error) {
	defineFormats()
	// Match on paths only; the servers list names deployments, not this process.
	matchDoc := *doc
	matchDoc.Servers = nil
	matchDoc.Paths = openapi3.NewPaths()
	for path, item := range doc.Paths.Map() {
		kept := *item
		kept.AdditionalOperations = maps.Clone(item.AdditionalOperations)
		for method := range item.Operations() {
			if !slices.Contains(served, Route{Method: strings.ToUpper(method), Path: path}) {
				kept.SetOperation(method, nil)
			}
		}
		if len(kept.Operations()) > 0 {
			matchDoc.Paths.Set(path, &kept)
		}
	}
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
				body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
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
// which may be a password, a token or a TOTP code, and causes are logged.
func toAppError(err error) error {
	fields := map[string]string{}
	for _, e := range multiErrors(err) {
		reqErr, ok := errors.AsType[*openapi3filter.RequestError](e)
		if !ok {
			fields["request"] = "invalid"
			continue
		}
		if _, ok = errors.AsType[*openapi3filter.ParseError](reqErr.Err); ok {
			return ErrMalformedJSON
		}
		field := "body"
		if reqErr.Parameter != nil {
			field = reqErr.Parameter.Name
		}
		for _, se := range multiErrors(reqErr.Err) {
			if _, isSchema := errors.AsType[*openapi3.SchemaError](se); !isSchema {
				// A missing required body or parameter.
				fields[field] = "required"
				continue
			}
			addProblems(fields, field, se)
		}
	}
	out := *errInvalidRequest
	out.Fields = fields
	return &out
}

// addProblems puts the field problem of the schema error err in fields. A
// JSON Schema 2020-12 error with several causes, such as an object with two
// bad properties beside another problem in the body, names no keyword; its
// causes go in its place, so a problem is never empty. An unknown property
// is keyed by its own path, inner.extra for one inside inner, and a
// dependentRequired miss by each property it lacks, as required.
func addProblems(fields map[string]string, field string, err error) {
	schemaErr, ok := errors.AsType[*openapi3.SchemaError](err)
	if !ok {
		fields[field] = "invalid"
		return
	}
	if schemaErr.SchemaField == "" && schemaErr.Origin != nil {
		for _, cause := range multiErrors(schemaErr.Origin) {
			addProblems(fields, field, cause)
		}
		return
	}
	path := schemaErr.JSONPointer()
	if v, isV := errors.AsType[*jsonschema.ValidationError](schemaErr); isV {
		for len(v.Causes) == 1 {
			v = v.Causes[0]
		}
		// kin-openapi's field is the property that needs the others.
		if dep, isDep := v.ErrorKind.(*kind.DependentRequired); isDep && dep.Prop == schemaErr.SchemaField {
			for _, missing := range dep.Missing {
				fields[strings.Join(slices.Concat(path, []string{missing}), ".")] = "required"
			}
			return
		}
	}
	name := field
	if len(path) > 0 {
		name = strings.Join(path, ".")
	}
	problem := schemaErr.SchemaField
	if problem == "" {
		problem = "invalid"
	}
	if extra := quotedName(schemaErr.Reason); problem == "additionalProperties" && extra != "" {
		name = strings.Join(slices.Concat(path, []string{extra}), ".")
	}
	fields[name] = problem
}

// multiErrors returns the errors of the openapi3.MultiError in err's chain,
// or err alone when there is none.
func multiErrors(err error) []error {
	if me, ok := errors.AsType[openapi3.MultiError](err); ok {
		return me
	}
	return []error{err}
}

// quotedName pulls the property out of kin-openapi's
// "additional properties 'extra' not allowed", or returns "".
func quotedName(reason string) string {
	_, rest, ok := strings.Cut(reason, "properties '")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "'")
	return name
}
