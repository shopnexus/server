package bifrost

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/llm"
)

// passthroughHeader lets fields Bifrost does not know, such as OpenRouter's
// provider pin, reach the provider.
const passthroughHeader = "x-bf-passthrough-extra-params"

// Caller calls models through the gateway as one caller, with its key and
// route from the ring.
type Caller struct {
	BaseURL string // the gateway's, without /v1
	Name    string
	Ring    *Ring
	HTTP    *http.Client
}

// call runs f with the caller's route. When Bifrost refuses the key (401 or
// 403), the reconciler may have changed it: the ring reads Bifrost again
// and f runs once more if the route changed.
func (c Caller) call(ctx context.Context, f func(Route) error) error {
	route, err := c.Ring.Route(ctx, c.Name)
	if err != nil {
		return err
	}
	err = f(route)
	var st interface{ HTTPStatus() int }
	if !errors.As(err, &st) || (st.HTTPStatus() != http.StatusUnauthorized && st.HTTPStatus() != http.StatusForbidden) {
		return err
	}
	if c.Ring.Reload(ctx) != nil {
		return err
	}
	again, rerr := c.Ring.Route(ctx, c.Name)
	if rerr != nil || reflect.DeepEqual(again, route) {
		return err
	}
	return f(again)
}

func (c Caller) header(r Route) http.Header {
	if len(r.ExtraBody) == 0 {
		return nil
	}
	return http.Header{passthroughHeader: {"true"}}
}

func (c Caller) base() string { return strings.TrimRight(c.BaseURL, "/") + "/v1" }

// Chat is an llm.Client and llm.Streamer over the gateway.
type Chat struct {
	Caller
	// Effort is sent as reasoning_effort; empty leaves the model's default.
	Effort string
}

var (
	_ llm.Client   = Chat{}
	_ llm.Streamer = Chat{}
	_ embed.Model  = Embedder{}
)

func (c Chat) client(r Route) llm.OpenAI {
	return llm.OpenAI{
		BaseURL: c.base(), APIKey: r.Key, Model: r.Model, Effort: c.Effort, Fallbacks: r.Fallbacks,
		Extra: r.ExtraBody, Header: c.header(r), HTTP: c.HTTP,
	}
}

func (c Chat) Complete(ctx context.Context, r llm.Request, out any) error {
	return c.call(ctx, func(rt Route) error { return c.client(rt).Complete(ctx, r, out) })
}

func (c Chat) Stream(ctx context.Context, r llm.Request, onText func(string)) (string, error) {
	var text string
	err := c.call(ctx, func(rt Route) error {
		var err error
		text, err = c.client(rt).Stream(ctx, r, onText)
		return err
	})
	return text, err
}

// Embedder is an embed.Model over the gateway.
type Embedder struct {
	Caller
	Dims int // the length every vector must have
}

func (e Embedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	var out [][]float32
	err := e.call(ctx, func(rt Route) error {
		var err error
		out, err = embed.OpenAI{
			BaseURL: e.base(), APIKey: rt.Key, Model: rt.Model, Dims: e.Dims, Fallbacks: rt.Fallbacks,
			Extra: rt.ExtraBody, Header: e.header(rt), HTTP: e.HTTP,
		}.Embed(ctx, inputs)
		return err
	})
	return out, err
}
