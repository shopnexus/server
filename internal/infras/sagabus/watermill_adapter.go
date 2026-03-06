package sagabus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ThreeDotsLabs/watermill/message"
)

// WatermillPublisherAdapter adapts watermill's message.Publisher to the custom Publisher interface
type WatermillPublisherAdapter struct {
	publisher message.Publisher
}

// NewWatermillPublisherAdapter creates a new adapter for watermill publisher
func NewWatermillPublisherAdapter(publisher message.Publisher) *WatermillPublisherAdapter {
	return &WatermillPublisherAdapter{
		publisher: publisher,
	}
}

// Publish implements the Publisher interface by converting custom Messages to watermill messages
func (a *WatermillPublisherAdapter) Publish(topic string, messages ...*Message) error {
	for _, msg := range messages {
		// Serialize the custom Message to JSON
		payload, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}

		// Create a watermill message with the custom message ID
		watermillMsg := message.NewMessage(msg.ID, payload)

		// Publish using watermill publisher
		if err := a.publisher.Publish(topic, watermillMsg); err != nil {
			return fmt.Errorf("failed to publish message to topic %s: %w", topic, err)
		}
	}
	return nil
}

// WatermillSubscriberAdapter adapts watermill's message.Subscriber to the custom Subscriber interface
type WatermillSubscriberAdapter struct {
	subscriber message.Subscriber
}

// NewWatermillSubscriberAdapter creates a new adapter for watermill subscriber
func NewWatermillSubscriberAdapter(subscriber message.Subscriber) *WatermillSubscriberAdapter {
	return &WatermillSubscriberAdapter{
		subscriber: subscriber,
	}
}

// Subscribe implements the Subscriber interface by converting watermill messages to custom Messages
func (a *WatermillSubscriberAdapter) Subscribe(ctx context.Context, topic string) (<-chan *Message, error) {
	// Subscribe using watermill subscriber
	watermillMessages, err := a.subscriber.Subscribe(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
	}

	// Create a channel for custom Messages
	msgChan := make(chan *Message)

	// Start a goroutine to convert watermill messages to custom messages
	go func() {
		defer close(msgChan)
		for {
			select {
			case watermillMsg, ok := <-watermillMessages:
				if !ok {
					// Channel closed
					return
				}
				if watermillMsg == nil {
					continue
				}

				// Deserialize the payload to custom Message
				var customMsg Message
				if err := json.Unmarshal(watermillMsg.Payload, &customMsg); err != nil {
					// If unmarshal fails, nack the message
					watermillMsg.Nack()
					continue
				}

				// Use the watermill message ID if custom message ID is empty
				if customMsg.ID == "" {
					customMsg.ID = watermillMsg.UUID
				}

				// Send the custom message
				select {
				case msgChan <- &customMsg:
					// Message sent successfully, ack the watermill message
					watermillMsg.Ack()
				case <-ctx.Done():
					// Context cancelled, nack and return
					watermillMsg.Nack()
					return
				}
			case <-ctx.Done():
				// Context cancelled
				return
			}
		}
	}()

	return msgChan, nil
}
