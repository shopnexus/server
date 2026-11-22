package httpx

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Route is one registered method and path, compared against the OpenAPI
// operations so neither side has an entry the other lacks.
type Route struct {
	Method, Path string
}

// Router is an [http.ServeMux] that remembers what was registered.
type Router struct {
	mux    *http.ServeMux
	routes []Route
}

func NewRouter() *Router { return &Router{mux: http.NewServeMux()} }

// Handle registers h for a pattern of the form "POST /v1/auth/sign-in".
func (r *Router) Handle(pattern string, h http.Handler) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		panic("httpx: pattern needs a method: " + pattern)
	}
	r.mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		noteRoute(req, path)
		h.ServeHTTP(w, req)
	}))
	r.routes = append(r.routes, Route{Method: method, Path: path})
}

func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) { r.Handle(pattern, h) }

// Route is the path pattern of the route that serves method and path, or ""
// when none does.
func (r *Router) Route(method, path string) string {
	_, pattern := r.mux.Handler(&http.Request{Method: method, URL: &url.URL{Path: path}})
	_, route, _ := strings.Cut(pattern, " ")
	return route
}

// Routes returns the registered routes in registration order.
func (r *Router) Routes() []Route { return slices.Clone(r.routes) }

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) { r.mux.ServeHTTP(w, req) }
