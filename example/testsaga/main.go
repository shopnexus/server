package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"

	"shopnexus-remastered/internal/infras/sagabus"
)

// 1. Prepare funcs for registering

// module order:
type CreateOrderParams struct {
	OrderID string
}

type OrderCreated struct {
	TanKhoOrderID string
}

func CreateOrder(ctx context.Context, params CreateOrderParams) (OrderCreated, error) {
	fmt.Printf("[CreateOrder] Processing order: %s\n", params.OrderID)
	// Simulate work
	return OrderCreated{TanKhoOrderID: "TK-" + params.OrderID}, nil
}

var OpCreateOrder = sagabus.NewOperation[CreateOrderParams, OrderCreated]("order.create")

type CancelOrderParams struct {
	OrderID string
}

func CancelOrder(ctx context.Context, params CancelOrderParams) (any, error) {
	fmt.Printf("[CancelOrder] Cancelling order: %s\n", params.OrderID)
	return nil, nil
}

var OpCancelOrder = sagabus.NewOperation[CancelOrderParams, any]("order.cancel")

// module inventory:

type ReserveInventoryParams struct {
	TanKhoOrderID string
}

type Inventory struct {
	Reserved bool
}

func ReserveInventory(ctx context.Context, params ReserveInventoryParams) (Inventory, error) {
	fmt.Printf("[ReserveInventory] Reserving for: %s\n", params.TanKhoOrderID)
	return Inventory{Reserved: true}, nil
}

var OpReserveInventory = sagabus.NewOperation[ReserveInventoryParams, Inventory]("inventory.reserve")

func main() {
	// 0. Setup Watermill GoChannel (In-memory Pub/Sub)
	logger := watermill.NewStdLogger(false, false)
	pubSub := gochannel.NewGoChannel(gochannel.Config{}, logger)

	// Adapters
	publisher := sagabus.NewWatermillPublisherAdapter(pubSub)
	subscriber := sagabus.NewWatermillSubscriberAdapter(pubSub)

	// SagaBus
	bus := sagabus.NewSagaBus(publisher, subscriber)

	// 2. Register operations

	sagabus.Register(bus, "order.create", CreateOrder, func(ctx context.Context, params CreateOrderParams, result OrderCreated) error {
		return nil
	})

	sagabus.Register(bus, "inventory.reserve", ReserveInventory, func(ctx context.Context, params ReserveInventoryParams, result Inventory) error {
		return nil
	})

	// 3. Create a route with data transformation
	// When OrderCreated -> Trigger ReserveInventory
	sagabus.Route(bus, OpCreateOrder, OpReserveInventory, func(result OrderCreated) (ReserveInventoryParams, error) {
		fmt.Printf("[Route] Transforming OrderCreated (%s) -> ReserveInventoryParams\n", result.TanKhoOrderID)
		return ReserveInventoryParams{
			TanKhoOrderID: result.TanKhoOrderID,
		}, nil
	})

	// 4. Test publish (Start the Saga)
	fmt.Println("Starting Saga...")
	err := sagabus.Publish(bus, OpCreateOrder, CreateOrderParams{OrderID: "12345"})
	if err != nil {
		panic(err)
	}

	// Wait for async processing
	time.Sleep(2 * time.Second)
	fmt.Println("Done.")
}
