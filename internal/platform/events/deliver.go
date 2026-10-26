package events

import (
	restate "github.com/restatedev/sdk-go"
)

const (
	deliverService = "Events"
	deliverHandler = "Deliver"
)

// deliverer is the Restate service Bus sends every event to, once.
type deliverer struct{ bus *Bus }

func (d deliverer) ServiceName() string { return d.bus.service() }

// Deliver hands e to every subscriber of its kind. Each send is journaled,
// so a retried Deliver sends nothing twice, and each subscriber runs as an
// invocation of its own that Restate retries alone. A send to a service
// Restate does not know yet fails this invocation, which Restate retries
// with backoff until the service is registered.
func (d deliverer) Deliver(ctx restate.Context, e Event) error {
	for _, s := range d.bus.subscribers(e.Kind) {
		restate.ServiceSend(ctx, s.Service, s.Handler).Send(e)
	}
	return nil
}
