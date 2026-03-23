package main

import (
	"fmt"
	"log"
	"time"

	"shopnexus-server/internal/infras/pubsub"

	"github.com/bytedance/sonic"
)

type Order struct {
	ID       string `json:"id"`
	Item     string `json:"item"`
	Quantity int    `json:"quantity"`
}

func main() {
	// Example usage of the NATS client
	cfg := pubsub.NatsConfig{
		Config: pubsub.Config{
			Brokers: []string{"localhost:4222"},
			Timeout: 10 * time.Second,
			Decoder: sonic.Unmarshal,
			Encoder: sonic.Marshal,
		},
		Group: "test-group",
	}

	client, err := pubsub.NewNatsClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create NATS client: %v", err)
	}

	go func() {
		for {
			time.Sleep(5 * time.Second)
			if err = client.Publish("orders", Order{
				ID:       "order" + time.Now().Format("20060102150405"),
				Item:     "Laptop",
				Quantity: 1,
			}); err != nil {
				log.Fatalf("Failed to publish message: %v", err)
			}
			if err = client.Publish("orders", Order{
				ID:       "order" + time.Now().Format("20060102150405"),
				Item:     "Laptop",
				Quantity: 1,
			}); err != nil {
				log.Fatalf("Failed to publish message: %v", err)
			}
			fmt.Println("Published order")
		}
	}()

	if err = client.Subscribe("orders", func(msg *pubsub.MessageDecoder) error {
		var order Order
		if err := msg.Decode(&order); err != nil {
			return err
		}
		log.Printf("Received order 1: %+v", order)
		return nil
	}); err != nil {
		log.Fatalf("Failed to subscribe to topic: %v", err)
	}

	log.Println("Subscribed to topic 'orders' successfully")

	select {} // Keep the main function running to listen for messages
}
