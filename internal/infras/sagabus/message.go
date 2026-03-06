package sagabus

import (
	"time"
)

// Message represents a message in the saga event bus
type Message struct {
	ID              string           `json:"id"`               // unique message id
	Payload         any              `json:"payload"`          // actual message payload
	OperationID     string           `json:"operation_id"`     // unique operation id or topic
	AllowRollback   bool             `json:"allow_rollback"`   // Allowing current op rollback or not
	Timestamp       time.Time        `json:"timestamp"`        // message creation time
	PreviousMessage *PreviousMessage `json:"previous_message"` // previous message id for compensation
	RollbackReason  *string          `json:"rollback_reason"`  // Rollback reason
}

type PreviousMessage struct {
	ID            string    `json:"id"`             // unique message id
	OperationID   string    `json:"operation_id"`   // unique operation id or topic
	AllowRollback bool      `json:"allow_rollback"` // Allowing current op rollback or not
	Timestamp     time.Time `json:"timestamp"`      // message creation time
}

type MessageDecoder struct {
	Raw     []byte
	decoder func(data []byte, v any) error
}

func NewMessageDecoder(raw []byte, decoder func(data []byte, v any) error) *MessageDecoder {
	return &MessageDecoder{
		Raw:     raw,
		decoder: decoder,
	}
}

// Decode into any struct, like json.Decoder
func (d *MessageDecoder) Decode(v any) error {
	return d.decoder(d.Raw, v)
}
