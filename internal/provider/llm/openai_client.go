package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Compile-time interface check.
var _ Client = (*OpenAIClient)(nil)

// OpenAIConfig holds configuration for the OpenAI client.
type OpenAIConfig struct {
	APIKey     string `yaml:"apiKey"`
	BaseURL    string `yaml:"baseURL"`    // optional, for custom endpoints
	EmbedModel string `yaml:"embedModel"` // e.g. "text-embedding-3-small"
	ChatModel  string `yaml:"chatModel"`  // e.g. "gpt-4o"
}

// OpenAIClient implements the Client interface using the official OpenAI Go SDK.
type OpenAIClient struct {
	client     *openai.Client
	embedModel string
	chatModel  string
}

// NewOpenAIClient creates a new OpenAIClient with the given configuration.
func NewOpenAIClient(cfg OpenAIConfig) *OpenAIClient {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	client := openai.NewClient(opts...)
	return &OpenAIClient{
		client:     &client,
		embedModel: cfg.EmbedModel,
		chatModel:  cfg.ChatModel,
	}
}

// Embed sends texts to the OpenAI embeddings API and returns dense vectors.
// OpenAI only returns dense embeddings; Sparse will be nil.
func (c *OpenAIClient) Embed(ctx context.Context, texts []string) ([]EmbedResult, error) {
	resp, err := c.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: c.embedModel,
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("llm/openai: embed: %w", err)
	}

	out := make([]EmbedResult, len(resp.Data))
	for i, d := range resp.Data {
		f32 := make([]float32, len(d.Embedding))
		for j, v := range d.Embedding {
			f32[j] = float32(v)
		}
		out[i] = EmbedResult{
			Dense:  f32,
			Sparse: nil,
		}
	}
	return out, nil
}

// GenerateText calls the chat completions API with a single user message.
func (c *OpenAIClient) GenerateText(ctx context.Context, params GenerateTextParams) (string, error) {
	p := openai.ChatCompletionNewParams{
		Model: c.chatModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(params.Prompt),
		},
	}
	if params.MaxTokens > 0 {
		p.MaxTokens = openai.Int(int64(params.MaxTokens))
	}
	if params.Temperature > 0 {
		p.Temperature = openai.Float(params.Temperature)
	}

	resp, err := c.client.Chat.Completions.New(ctx, p)
	if err != nil {
		return "", fmt.Errorf("llm/openai: generate text: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("llm/openai: generate text: no choices returned")
	}
	return resp.Choices[0].Message.Content, nil
}

// Chat calls the chat completions API mapping our ChatMessage roles to SDK types.
func (c *OpenAIClient) Chat(ctx context.Context, params ChatParams) (ChatMessage, error) {
	msgs := make([]openai.ChatCompletionMessageParamUnion, len(params.Messages))
	for i, m := range params.Messages {
		switch m.Role {
		case RoleSystem:
			msgs[i] = openai.SystemMessage(m.Content)
		case RoleUser:
			msgs[i] = openai.UserMessage(m.Content)
		case RoleAssistant:
			msgs[i] = openai.AssistantMessage(m.Content)
		default:
			msgs[i] = openai.UserMessage(m.Content)
		}
	}

	p := openai.ChatCompletionNewParams{
		Model:    c.chatModel,
		Messages: msgs,
	}
	if params.MaxTokens > 0 {
		p.MaxTokens = openai.Int(int64(params.MaxTokens))
	}
	if params.Temperature > 0 {
		p.Temperature = openai.Float(params.Temperature)
	}

	resp, err := c.client.Chat.Completions.New(ctx, p)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("llm/openai: chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return ChatMessage{}, errors.New("llm/openai: chat: no choices returned")
	}
	msg := resp.Choices[0].Message
	return ChatMessage{
		Role:    RoleAssistant,
		Content: msg.Content,
	}, nil
}

// GenerateStructured calls the chat completions API with JSON object response format.
// The schema is expected to be embedded in the prompt by the caller.
func (c *OpenAIClient) GenerateStructured(
	ctx context.Context,
	params GenerateStructuredParams,
) (json.RawMessage, error) {
	p := openai.ChatCompletionNewParams{
		Model: c.chatModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(params.Prompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &openai.ResponseFormatJSONObjectParam{},
		},
	}
	if params.MaxTokens > 0 {
		p.MaxTokens = openai.Int(int64(params.MaxTokens))
	}
	if params.Temperature > 0 {
		p.Temperature = openai.Float(params.Temperature)
	}

	resp, err := c.client.Chat.Completions.New(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("llm/openai: generate structured: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("llm/openai: generate structured: no choices returned")
	}
	return json.RawMessage(resp.Choices[0].Message.Content), nil
}
