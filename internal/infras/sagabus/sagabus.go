package sagabus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SagaBus orchestrates saga operations with automatic rollback on failures
type SagaBus struct {
	publisher  Publisher
	subscriber Subscriber
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewSagaBus creates a new saga bus
func NewSagaBus(publisher Publisher, subscriber Subscriber) *SagaBus {
	ctx, cancel := context.WithCancel(context.Background())
	return &SagaBus{
		publisher:  publisher,
		subscriber: subscriber,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Close shuts down the saga bus
func (sb *SagaBus) Close() {
	sb.cancel()
}

// Register registers an operation and starts listening for messages
func Register[Params, Result any](
	bus *SagaBus,
	operationID string,
	handler Handler[Params, Result],
	compensater func(ctx context.Context, params Params, result Result) error,
) {
	// Subscribe to operation messages
	go subscribe(bus, operationID, func(ctx context.Context, msg *Message) error {
		params, err := decode[Params](msg.Payload)
		if err != nil {
			return err
		}

		result, err := handler(ctx, params)
		if err != nil {
			rollback(bus, msg, fmt.Sprintf("operation error: %v", err))
			return err
		}

		// Publish done message
		if err := publish(bus, operationID+".done", result, msg.AllowRollback, &PreviousMessage{
			ID:            msg.ID,
			OperationID:   msg.OperationID,
			AllowRollback: msg.AllowRollback,
			Timestamp:     msg.Timestamp,
		}); err != nil {
			return err
		}

		return nil
	})

	// Subscribe to rollback messages
	go subscribe(bus, operationID+".rollback", func(ctx context.Context, msg *Message) error {
		params, _ := decode[Params](msg.Payload)
		var result Result

		if compensater != nil {
			if err := compensater(ctx, params, result); err != nil {
				fmt.Printf("[Rollback Error] %s: %v\n", operationID, err)
			} else {
				fmt.Printf("[Rollback OK] %s\n", operationID)
			}
		}

		// Continue rollback chain
		if msg.PreviousMessage != nil && msg.PreviousMessage.AllowRollback {
			var reason = fmt.Sprintf("cascading rollback from %s, reason: %s", operationID, *msg.RollbackReason)
			publishRollback(bus, msg.PreviousMessage.OperationID, msg.Payload, &reason, msg.PreviousMessage)
		}

		// create order -> reserverve inv -> process payment
		// create order -> reserverve inv -> process payment X
		// create order -> reserverve inv rollback -> order rollback

		return nil
	})
}

// Route connects two operations: when "from" completes, trigger "to" with transformed data
func Route[FromParams, FromResult, ToParams, ToResult any](
	bus *SagaBus,
	from Operation[FromParams, FromResult],
	to Operation[ToParams, ToResult],
	transform func(FromResult) (ToParams, error),
) {
	// Subscribe to "from" operation to capture its result
	go subscribe(bus, from.ID+".done", func(ctx context.Context, msg *Message) error {
		result, err := decode[FromResult](msg.Payload)
		if err != nil {
			rollback(bus, msg, fmt.Sprintf("decode error: %v", err))
			return err
		}

		nextParams, err := transform(result)
		if err != nil {
			rollback(bus, msg, fmt.Sprintf("transform error: %v", err))
			return err
		}

		// Publish to next operation
		return publish(bus, to.ID, nextParams, true, &PreviousMessage{
			ID:            msg.ID,
			OperationID:   from.ID,
			AllowRollback: msg.AllowRollback,
			Timestamp:     msg.Timestamp,
		})
	})
}

// Publish starts a saga by publishing to an operation
func Publish[Params any, Result any](bus *SagaBus, op Operation[Params, Result], params Params) error {
	return publish(bus, op.ID, params, true, nil)
}

// --- Internal helpers ---

func subscribe(bus *SagaBus, topic string, handler func(context.Context, *Message) error) {
	msgChan, err := bus.subscriber.Subscribe(bus.ctx, topic)
	if err != nil {
		fmt.Printf("[Subscribe Error] %s: %v\n", topic, err)
		return
	}

	for {
		select {
		case <-bus.ctx.Done():
			return
		case msg, ok := <-msgChan:
			if !ok {
				return
			}
			if err := handler(bus.ctx, msg); err != nil {
				fmt.Printf("[Handler Error] %s: %v\n", topic, err)
			}
		}
	}
}

func publish(bus *SagaBus, topic string, payload any, allowRollback bool, prev *PreviousMessage) error {
	msg := &Message{
		ID:              uuid.New().String(),
		Payload:         payload,
		OperationID:     topic,
		AllowRollback:   allowRollback,
		Timestamp:       time.Now(),
		PreviousMessage: prev,
	}
	return bus.publisher.Publish(topic, msg)
}

func publishRollback(bus *SagaBus, operationID string, payload any, reason *string, prev *PreviousMessage) {
	msg := &Message{
		ID:              uuid.New().String(),
		Payload:         payload,
		OperationID:     operationID + ".rollback",
		AllowRollback:   true,
		Timestamp:       time.Now(),
		PreviousMessage: prev,
		RollbackReason:  reason,
	}
	if err := bus.publisher.Publish(operationID+".rollback", msg); err != nil {
		fmt.Printf("[Publish Rollback Error] %s: %v\n", operationID, err)
	}
}

func rollback(bus *SagaBus, msg *Message, reason string) {
	if !msg.AllowRollback {
		return
	}
	fmt.Printf("[Rollback] %s - Reason: %s\n", msg.OperationID, reason)
	publishRollback(bus, msg.OperationID, msg.Payload, &reason, msg.PreviousMessage)
}

func decode[T any](data any) (T, error) {
	var result T
	bytes, err := json.Marshal(data)
	if err != nil {
		return result, fmt.Errorf("marshal error: %w", err)
	}
	if err := json.Unmarshal(bytes, &result); err != nil {
		return result, fmt.Errorf("unmarshal error: %w", err)
	}
	return result, nil
}
