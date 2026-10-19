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
)

// dedupWindow is how long JetStream remembers Nats-Msg-Id values; a relay
// that republishes within it does not create a duplicate.
const dedupWindow = 10 * time.Minute

// EnsureStreams creates or updates the three streams.
func EnsureStreams(ctx context.Context, js jetstream.JetStream) error {
	configs := []jetstream.StreamConfig{
		{Name: streamCmd, Subjects: []string{"cmd.>"}, Retention: jetstream.WorkQueuePolicy, Duplicates: dedupWindow, Storage: jetstream.FileStorage},
		{Name: streamEvt, Subjects: []string{"evt.>"}, Retention: jetstream.LimitsPolicy, MaxAge: 7 * 24 * time.Hour, Duplicates: dedupWindow, Storage: jetstream.FileStorage},
		{Name: streamDLQ, Subjects: []string{"dlq.>"}, Retention: jetstream.LimitsPolicy, MaxAge: 30 * 24 * time.Hour, Storage: jetstream.FileStorage},
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
