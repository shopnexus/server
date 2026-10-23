package httpx

import (
	"encoding"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"sync"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// ErrMalformedJSON is a body that does not parse as the expected JSON.
var ErrMalformedJSON = apperr.New(apperr.KindBadRequest, "malformed_json", "request body is not valid JSON")

// Bind reads the request into dst. A field tagged `path`, `query` or
// `header` takes the request value of that name and must also be tagged
// `json:"-"`. The other fields come from the JSON body, which is read only
// when T has such a field.
//
// ValidateRequests has already checked every value against the spec,
// defaults included, so Bind converts strings to Go types and checks no
// shape rule. Unknown body fields are rejected here as well so a handler
// outside the spec cannot accept them silently.
func Bind[T any](r *http.Request, dst *T) error {
	p := planOf(reflect.TypeFor[T]())
	if p.body {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(dst); err != nil {
			e := *ErrMalformedJSON
			e.Err = err
			return &e
		}
	}
	rv := reflect.ValueOf(dst).Elem()
	var query url.Values
	for _, f := range p.fields {
		var raw string
		switch f.source {
		case sourcePath:
			raw = r.PathValue(f.name)
		case sourceQuery:
			if query == nil {
				query = r.URL.Query()
			}
			raw = query.Get(f.name)
		case sourceHeader:
			raw = r.Header.Get(f.name)
		default:
			panic(fmt.Sprintf("httpx: unknown source %d", f.source))
		}
		if raw == "" {
			continue
		}
		if err := f.parse(rv.FieldByIndex(f.index), raw); err != nil {
			e := *errInvalidRequest.WithField(f.name, "invalid")
			e.Err = err
			return &e
		}
	}
	return nil
}

type source int

const (
	sourcePath source = iota
	sourceQuery
	sourceHeader
)

// sourceTags is the struct tag for each source.
var sourceTags = [...]string{sourcePath: "path", sourceQuery: "query", sourceHeader: "header"}

// plan is what Bind needs to know about a request type, worked out from its
// struct tags on the first request.
type plan struct {
	fields []field
	body   bool
}

type field struct {
	parse  func(v reflect.Value, raw string) error
	name   string
	index  []int // for reflect.Value.FieldByIndex
	source source
}

// plans maps a [reflect.Type] to its *plan.
var plans sync.Map

func planOf(t reflect.Type) *plan {
	v, ok := plans.Load(t)
	if !ok {
		// Concurrent first requests may each build a plan; LoadOrStore keeps
		// the first one stored and every caller gets it.
		v, _ = plans.LoadOrStore(t, newPlan(t))
	}
	p, ok := v.(*plan)
	if !ok {
		panic(fmt.Sprintf("httpx: plans holds a %T", v))
	}
	return p
}

// newPlan reads an embedded struct without a json tag, such as page.Request,
// as part of t. It panics on a tag or field type it does not support. Every
// route has an e2e test, which then fails on the route's first request.
func newPlan(t reflect.Type) *plan {
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("httpx: Bind needs a struct, got %s", t))
	}
	p := &plan{fields: nil, body: false}
	for i := range t.NumField() {
		sf := t.Field(i)
		f := field{parse: nil, name: "", index: []int{i}, source: 0}
		found := 0
		for src, key := range sourceTags {
			if name, ok := sf.Tag.Lookup(key); ok {
				f.name, f.source = name, source(src)
				found++
			}
		}
		switch {
		case found == 0 && sf.Anonymous && sf.Type.Kind() == reflect.Struct && sf.Tag.Get("json") == "":
			inner := newPlan(sf.Type)
			for _, g := range inner.fields {
				g.index = append([]int{i}, g.index...)
				p.fields = append(p.fields, g)
			}
			p.body = p.body || inner.body
		case found == 0:
			if sf.IsExported() && sf.Tag.Get("json") != "-" {
				p.body = true
			}
		case found > 1:
			panic(fmt.Sprintf("httpx: %s.%s has more than one of path, query and header", t, sf.Name))
		case sf.Tag.Get("json") != "-":
			panic(fmt.Sprintf("httpx: %s.%s comes from the URL or a header and needs json:\"-\"", t, sf.Name))
		default:
			f.parse = parserFor(t, sf)
			p.fields = append(p.fields, f)
		}
	}
	return p
}

var textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()

func parserFor(t reflect.Type, sf reflect.StructField) func(reflect.Value, string) error {
	ft := sf.Type
	if reflect.PointerTo(ft).Implements(textUnmarshaler) {
		return func(v reflect.Value, raw string) error {
			u, ok := reflect.TypeAssert[encoding.TextUnmarshaler](v.Addr())
			if !ok {
				return fmt.Errorf("%s is not a TextUnmarshaler", ft)
			}
			if err := u.UnmarshalText([]byte(raw)); err != nil {
				return fmt.Errorf("parse %s: %w", ft, err)
			}
			return nil
		}
	}
	// Bind reads four of the 27 kinds; the rest panic.
	//
	//exhaustive:ignore
	switch ft.Kind() {
	case reflect.String:
		return func(v reflect.Value, raw string) error {
			v.SetString(raw)
			return nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return func(v reflect.Value, raw string) error {
			n, err := strconv.ParseInt(raw, 10, ft.Bits())
			if err != nil {
				return fmt.Errorf("parse int: %w", err)
			}
			v.SetInt(n)
			return nil
		}
	case reflect.Bool:
		return func(v reflect.Value, raw string) error {
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("parse bool: %w", err)
			}
			v.SetBool(b)
			return nil
		}
	default:
		panic(fmt.Sprintf("httpx: %s.%s has type %s, which Bind cannot read from a string", t, sf.Name, ft))
	}
}
