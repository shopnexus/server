package httpx

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
)

// None is a request or an answer without fields.
type None struct{}

// Serve runs one endpoint in the handlers' order: Bind for the request,
// the use case, then Respond with status, or WriteError. A 204 writes no
// body.
func Serve[In, Out any](w http.ResponseWriter, r *http.Request, log *slog.Logger, status int, run func(in In) (Out, error)) {
	var in In
	if err := Bind(r, &in); err != nil {
		WriteError(w, r, log, err)
		return
	}
	out, err := run(in)
	if err != nil {
		WriteError(w, r, log, err)
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	Respond(w, status, out)
}

// ServeCaller is Serve on a signed-in route: the caller comes first, and
// a request without one gets 401.
func ServeCaller[In, Out any](w http.ResponseWriter, r *http.Request, log *slog.Logger, status int,
	run func(p authn.Principal, in In) (Out, error),
) {
	p, ok := Caller(w, r, log)
	if !ok {
		return
	}
	Serve(w, r, log, status, func(in In) (Out, error) { return run(p, in) })
}

// ServeConsole is Serve on a console route, with the console member first.
func ServeConsole[In, Out any](w http.ResponseWriter, r *http.Request, log *slog.Logger, status int,
	run func(m authn.ConsoleMember, in In) (Out, error),
) {
	m, ok := ConsoleCaller(w, r, log)
	if !ok {
		return
	}
	Serve(w, r, log, status, func(in In) (Out, error) { return run(m, in) })
}
