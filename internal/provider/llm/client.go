package llm

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrNotSupported is returned when a provider does not implement a capability.
var ErrNotSupported = errors.New("llm: operation not supported by this provider")

// Client defines the interface for LLM providers.
type Client interface {
	Embed(ctx context.Context, texts []string) ([]EmbedResult, error)
	GenerateText(ctx context.Context, params GenerateTextParams) (string, error)
	Chat(ctx context.Context, params ChatParams) (ChatMessage, error)
	GenerateStructured(ctx context.Context, params GenerateStructuredParams) (json.RawMessage, error)
}
