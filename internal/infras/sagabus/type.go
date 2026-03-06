package sagabus

import "context"

type Publisher interface {
	Publish(topic string, messages ...*Message) error
}

type Subscriber interface {
	Subscribe(ctx context.Context, topic string) (<-chan *Message, error)
}

// For the operations
type Handler[Params, Result any] = func(context.Context, Params) (Result, error)
type Middleware = func(any) (any, error)
type Operation[Params, Result any] struct {
	ID        string
	DoHandler Handler[Params, Result]
}

func NewOperation[Params, Result any](id string) Operation[Params, Result] {
	return Operation[Params, Result]{
		ID: id,
		// DoHandler to be set during registration
	}
}
