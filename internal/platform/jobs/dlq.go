package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type deliveryAdvisory struct {
	Stream    string `json:"stream"`
	Consumer  string `json:"consumer"`
	StreamSeq uint64 `json:"stream_seq"`
}

// RunDLQ copies every CMD message that ran out of attempts to dlq.<subject>,
// removes it from its work queue, and logs an error. It reads the advisories
// captured in the ADV stream through one durable consumer, so several
// instances copy each message once, and advisories that fired while no
// instance ran are still handled. It blocks until ctx ends.
func RunDLQ(ctx context.Context, js jetstream.JetStream, log *slog.Logger) error {
	cons, err := js.CreateOrUpdateConsumer(ctx, streamAdv, jetstream.ConsumerConfig{
		Durable:   "dlq-copier",
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return err
	}
	cc, err := cons.Consume(func(m jetstream.Msg) {
		var adv deliveryAdvisory
		if err := json.Unmarshal(m.Data(), &adv); err != nil {
			log.Error("unreadable max-deliveries advisory", "err", err)
			m.Term()
			return
		}
		if err := deadLetter(ctx, js, log, adv); err != nil {
			log.Error("dead-letter copy failed; retrying", "stream", adv.Stream, "seq", adv.StreamSeq, "err", err)
			m.NakWithDelay(time.Second)
			return
		}
		m.Ack()
	})
	if err != nil {
		return err
	}
	<-ctx.Done()
	cc.Stop()
	return nil
}

// deadLetter is idempotent: the copy carries a message id the DLQ stream
// deduplicates, and a message already deleted counts as done.
func deadLetter(ctx context.Context, js jetstream.JetStream, log *slog.Logger, adv deliveryAdvisory) error {
	stream, err := js.Stream(ctx, adv.Stream)
	if err != nil {
		return err
	}
	raw, err := stream.GetMsg(ctx, adv.StreamSeq)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return nil
	}
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
	id := fmt.Sprintf("dlq-%s-%d", adv.Stream, adv.StreamSeq)
	if _, err := js.PublishMsg(ctx, msg, jetstream.WithMsgID(id)); err != nil {
		return err
	}
	log.ErrorContext(ctx, "job moved to the dead-letter queue",
		"subject", raw.Subject, "consumer", adv.Consumer, "seq", adv.StreamSeq)
	// A work-queue message that hit MaxDeliver stays in the stream; it has
	// been copied, so remove it.
	if err := stream.DeleteMsg(ctx, adv.StreamSeq); err != nil && !errors.Is(err, jetstream.ErrMsgNotFound) {
		return err
	}
	return nil
}
