package httpx

import (
	"net/http"
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
	r.mux.Handle(pattern, h)
	r.routes = append(r.routes, Route{Method: method, Path: path})
}

func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) { r.Handle(pattern, h) }

// Routes returns the registered routes in registration order.
func (r *Router) Routes() []Route { return append([]Route(nil), r.routes...) }

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) { r.mux.ServeHTTP(w, req) }
