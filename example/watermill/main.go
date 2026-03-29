// Sources for https://watermill.io/docs/getting-started/ (adapted for NATS JetStream)
package main

import (
	"context"
	"log"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	wnats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	nc "github.com/nats-io/nats.go"
)

func main() {
	logger := watermill.NewStdLogger(false, false)

	subscriber, err := wnats.NewSubscriber(
		wnats.SubscriberConfig{
			URL:              nc.DefaultURL,
			QueueGroupPrefix: "test_consumer_group",
			Unmarshaler:      &wnats.GobMarshaler{},
			JetStream: wnats.JetStreamConfig{
				AutoProvision: true,
				DurablePrefix: "test_consumer_group",
			},
		},
		logger,
	)
	if err != nil {
		panic(err)
	}

	messages, err := subscriber.Subscribe(context.Background(), "example.topic")
	if err != nil {
		panic(err)
	}

	go process(messages)

	publisher, err := wnats.NewPublisher(
		wnats.PublisherConfig{
			URL:       nc.DefaultURL,
			Marshaler: &wnats.GobMarshaler{},
			JetStream: wnats.JetStreamConfig{
				AutoProvision: true,
			},
		},
		logger,
	)
	if err != nil {
		panic(err)
	}

	publishMessages(publisher)
}

func publishMessages(publisher message.Publisher) {
	for {
		msg := message.NewMessage(watermill.NewUUID(), []byte("Hello, world!"))

		if err := publisher.Publish("example.topic", msg); err != nil {
			panic(err)
		}

		time.Sleep(time.Second)
	}
}

func process(messages <-chan *message.Message) {
	for msg := range messages {
		log.Printf("received message: %s, payload: %s", msg.UUID, string(msg.Payload))

		// we need to Acknowledge that we received and processed the message,
		// otherwise, it will be resent over and over again.
		msg.Ack()
	}
}
