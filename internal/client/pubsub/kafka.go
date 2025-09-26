package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/IBM/sarama"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-kafka/v3/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"

	"shopnexus-remastered/internal/logger"
)

type KafkaClient struct {
	config KafkaConfig
}

type KafkaConfig struct {
	Config
	Group string // Optional consumer group for subscriptions
}

// NewKafkaClient creates a new Kafka client using franz-go.
func NewKafkaClient(cfg KafkaConfig) (*KafkaClient, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("at least one broker must be specified")
	}
	if cfg.Decoder == nil {
		cfg.Decoder = json.Unmarshal // Default to JSON decoder
	}
	if cfg.Encoder == nil {
		cfg.Encoder = json.Marshal // Default to JSON encoder
	}

	return &KafkaClient{
		config: cfg,
	}, nil
}

// Group returns a new KafkaClient instance with the specified consumer group.
func (k *KafkaClient) Group(name string) Client {
	newConfig := k.config
	newConfig.Group = name
	return &KafkaClient{
		config: newConfig,
	}
}

// Publish sends a message to the given topic.
func (k *KafkaClient) Publish(topic string, value any) error {
	// Encode the value using the configured encoder
	encodedValue, err := k.config.Encoder(value)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	publisher, err := kafka.NewPublisher(
		kafka.PublisherConfig{
			Brokers:   k.config.Brokers,
			Marshaler: kafka.DefaultMarshaler{},
		},
		watermill.NewStdLogger(false, false),
	)
	if err != nil {
		return fmt.Errorf("failed to create publisher: %w", err)
	}

	if err = publisher.Publish(topic, message.NewMessage(watermill.NewUUID(), encodedValue)); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// Subscribe listens for messages on a topic and handles them with the given callback.
func (k *KafkaClient) Subscribe(topic string, handler func(msg *MessageDecoder) error) error {
	ctx := context.TODO()
	saramaSubscriberConfig := kafka.DefaultSaramaSubscriberConfig()
	// equivalent of auto.offset.reset: earliest
	saramaSubscriberConfig.Consumer.Offsets.Initial = sarama.OffsetOldest
	saramaSubscriberConfig.Consumer.Offsets.AutoCommit.Enable = false

	if k.config.Group == "" {
		logger.Log.Sugar().Warnf("Subscribing to topic %s without a consumer group", topic)
	}

	subscriber, err := kafka.NewSubscriber(
		kafka.SubscriberConfig{
			Brokers:               k.config.Brokers,
			Unmarshaler:           kafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaSubscriberConfig,
			ConsumerGroup:         k.config.Group,
		},
		watermill.NewStdLogger(false, false),
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}

	messages, err := subscriber.Subscribe(ctx, topic)
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
	}

	// Process messages continuously - launch in goroutine to handle context cancellation
	go func() {
		for {
			select {
			case msg := <-messages:
				if err := handler(NewMessageDecoder(msg.Payload, k.config.Decoder)); err != nil {
					msg.Nack()
					log.Printf("Error handling message: %v", err)
				} else {
					msg.Ack()
				}
			case <-ctx.Done():
				// Context cancelled, exit the loop
				log.Println("Context cancelled, stopping subscription")
				if err := subscriber.Close(); err != nil {
					log.Printf("Error closing subscriber: %v", err)
				}
			}
		}
	}()

	return nil
}

// Close cleanly shuts down the Kafka client and all consumers.
func (k *KafkaClient) Close() error {
	// Close the main client

	return nil
}
