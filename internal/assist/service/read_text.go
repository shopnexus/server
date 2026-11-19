package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/platform/relay"
	"github.com/shopnexus/server/internal/provider/llm"
)

// publishEvery is how often a read publishes its text while the model
// writes it.
const publishEvery = 100 * time.Millisecond

// Relay carries a read's text as the model writes it to the replicas
// whose sellers watch the read.
type Relay interface {
	Publish(ctx context.Context, key uuid.UUID, msg json.RawMessage) error
	Subscribe(key uuid.UUID) *relay.Sub
}

// relayKind names what a relayed message says.
type relayKind string

const (
	relayText relayKind = "text"
	// relayDone says the read was saved done or failed.
	relayDone relayKind = "done"
)

// relayed is a message about a read on the relay.
type relayed struct {
	Kind relayKind    `json:"kind"`
	Text *domain.Text `json:"text,omitempty"`
}

// writeText asks the model for a read's text and publishes what it has
// written so far, as partial makes it, at most every publishEvery. A model
// that refuses is llm.ErrRefused.
func (d Deps) writeText(ctx context.Context, read uuid.UUID, r llm.Request, partial func(string) domain.Text) (string, error) {
	modelCtx, cancel := context.WithTimeout(ctx, d.CallTimeout)
	defer cancel()
	var last time.Time
	var sent domain.Text
	text, err := d.Model.Stream(modelCtx, r, func(so string) {
		t := partial(so)
		if t == sent || time.Since(last) < publishEvery {
			return
		}
		last, sent = time.Now(), t
		d.publish(ctx, read, relayed{Kind: relayText, Text: &t})
	})
	if err != nil {
		return "", fmt.Errorf("ask the model: %w", err)
	}
	return text, nil
}

// publish sends m about read; a lost message costs the seller only a
// moment of the text, so it is logged.
func (d Deps) publish(ctx context.Context, read uuid.UUID, m relayed) {
	b, err := json.Marshal(m)
	if err == nil {
		err = d.Relay.Publish(ctx, read, b)
	}
	if err != nil {
		d.Log.WarnContext(ctx, "the read's text was not relayed", "read", read, "err", err)
	}
}

// relayEnd tells the read's watchers that it ended, as a step.
func (d Deps) relayEnd(ctx restate.Context, read uuid.UUID) {
	_ = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		d.publish(rc, read, relayed{Kind: relayDone})
		return nil
	})
}

// refusedModel reports a model that refused the request itself.
func refusedModel(err error) bool { return errors.Is(err, llm.ErrRefused) }
