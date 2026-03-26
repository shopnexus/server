package llm

import "encoding/json"

// EmbedResult holds the dense and sparse vectors produced by an embedding provider.
type EmbedResult struct {
	Dense  []float32          `json:"dense"`
	Sparse map[uint32]float32 `json:"sparse"`
}

// GenerateTextParams holds parameters for text generation.
type GenerateTextParams struct {
	Prompt        string   `json:"prompt"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
	Temperature   float64  `json:"temperature,omitempty"`
	StopSequences []string `json:"stop_sequences,omitempty"`
}

// Role represents the role of a chat message sender.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatParams holds parameters for chat completion.
type ChatParams struct {
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

// GenerateStructuredParams holds parameters for structured output generation.
type GenerateStructuredParams struct {
	Prompt      string          `json:"prompt"`
	Schema      json.RawMessage `json:"schema"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}
