package jobs

import (
	"context"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Stream names and the subjects they hold.
const (
	streamCmd = "CMD" // cmd.>: one consumer per subject, removed once handled
	streamEvt = "EVT" // evt.>: kept 7 days for many listeners
	streamDLQ = "DLQ" // dlq.>: messages that ran out of attempts, kept 30 days
	streamAdv = "ADV" // max-deliveries advisories for CMD, until the DLQ copier handles them
)

// maxDeliveries is the advisory JetStream publishes when a CMD consumer has
// tried a message MaxDeliver times. EVT is left out on purpose: an event
// belongs to every listener, so one listener giving up must not delete it.
const maxDeliveries = "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES." + streamCmd + ".*"

// dedupWindow is how long JetStream remembers Nats-Msg-Id values; a relay
// that republishes within it does not create a duplicate.
const dedupWindow = 10 * time.Minute

// EnsureStreams creates or updates the three streams.
func EnsureStreams(ctx context.Context, js jetstream.JetStream) error {
	configs := []jetstream.StreamConfig{
		{Name: streamCmd, Subjects: []string{"cmd.>"}, Retention: jetstream.WorkQueuePolicy, Duplicates: dedupWindow, Storage: jetstream.FileStorage},
		{Name: streamEvt, Subjects: []string{"evt.>"}, Retention: jetstream.LimitsPolicy, MaxAge: 7 * 24 * time.Hour, Duplicates: dedupWindow, Storage: jetstream.FileStorage},
		{Name: streamDLQ, Subjects: []string{"dlq.>"}, Retention: jetstream.LimitsPolicy, MaxAge: 30 * 24 * time.Hour, Duplicates: dedupWindow, Storage: jetstream.FileStorage},
		// Advisories are fire-and-forget; capturing them in a stream means
		// none is lost while no copier runs, and a durable consumer hands
		// each to exactly one instance.
		{Name: streamAdv, Subjects: []string{maxDeliveries}, Retention: jetstream.WorkQueuePolicy, MaxAge: 30 * 24 * time.Hour, Storage: jetstream.FileStorage},
	}
	for _, c := range configs {
		if _, err := js.CreateOrUpdateStream(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func streamFor(subject string) string {
	if strings.HasPrefix(subject, "evt.") {
		return streamEvt
	}
	return streamCmd
}
