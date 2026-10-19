package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// maxDeliveries is the advisory JetStream publishes when a consumer has
// tried a message MaxDeliver times without an acknowledgement.
const maxDeliveries = "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>"

type deliveryAdvisory struct {
	Stream    string `json:"stream"`
	Consumer  string `json:"consumer"`
	StreamSeq uint64 `json:"stream_seq"`
}

// RunDLQ copies every message that ran out of attempts to dlq.<subject>,
// removes it from its work queue, and logs an error. It blocks until ctx ends.
func RunDLQ(ctx context.Context, nc *nats.Conn, js jetstream.JetStream, log *slog.Logger) error {
	sub, err := nc.Subscribe(maxDeliveries, func(m *nats.Msg) {
		var adv deliveryAdvisory
		if err := json.Unmarshal(m.Data, &adv); err != nil {
			log.Error("unreadable max-deliveries advisory", "err", err)
			return
		}
		if err := deadLetter(ctx, js, adv); err != nil {
			log.Error("dead-letter copy failed", "stream", adv.Stream, "seq", adv.StreamSeq, "err", err)
		}
	})
	if err != nil {
		return err
	}
	<-ctx.Done()
	return sub.Unsubscribe()
}

func deadLetter(ctx context.Context, js jetstream.JetStream, adv deliveryAdvisory) error {
	stream, err := js.Stream(ctx, adv.Stream)
	if err != nil {
		return err
	}
	raw, err := stream.GetMsg(ctx, adv.StreamSeq)
	if err != nil {
		return err
	}
	msg := nats.NewMsg("dlq." + raw.Subject)
	msg.Data = raw.Data
	for k, vs := range raw.Header {
		for _, v := range vs {
			msg.Header.Add(k, v)
		}
	}
	msg.Header.Del(nats.MsgIdHdr)
	msg.Header.Set("Dlq-Consumer", adv.Consumer)
	msg.Header.Set("Dlq-Stream-Seq", strconv.FormatUint(adv.StreamSeq, 10))
	if _, err := js.PublishMsg(ctx, msg); err != nil {
		return err
	}
	slog.Default().ErrorContext(ctx, "job moved to the dead-letter queue",
		"subject", raw.Subject, "consumer", adv.Consumer, "seq", adv.StreamSeq)
	// A work-queue message that hit MaxDeliver stays in the stream; it has
	// been copied, so remove it.
	return stream.DeleteMsg(ctx, adv.StreamSeq)
}
