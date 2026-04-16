package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// Compile-time interface check.
var _ Client = (*BedrockClient)(nil)

// BedrockConfig holds configuration for the AWS Bedrock client.
type BedrockConfig struct {
	Region       string `yaml:"region"`
	EmbedModelID string `yaml:"embedModelId"` // e.g. "amazon.titan-embed-text-v2:0"
	ChatModelID  string `yaml:"chatModelId"`  // e.g. "anthropic.claude-3-haiku-20240307-v1:0"
}

// BedrockClient is an AWS Bedrock client implementing the llm.Client interface.
type BedrockClient struct {
	client *bedrockruntime.Client
	cfg    BedrockConfig
}

// NewBedrockClient creates a new BedrockClient by loading the default AWS config with the given region.
func NewBedrockClient(ctx context.Context, cfg BedrockConfig) (*BedrockClient, error) {
	sdkConfig, err := awsConfig.LoadDefaultConfig(ctx, awsConfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("llm/bedrock: load AWS config: %w", err)
	}
	return &BedrockClient{
		client: bedrockruntime.NewFromConfig(sdkConfig),
		cfg:    cfg,
	}, nil
}

// titanEmbedRequest is the JSON request body for Titan Text Embeddings V2.
type titanEmbedRequest struct {
	InputText  string `json:"inputText"`
	Dimensions int    `json:"dimensions"`
	Normalize  bool   `json:"normalize"`
}

// titanEmbedResponse is the JSON response body from Titan Text Embeddings V2.
type titanEmbedResponse struct {
	Embedding           []float32 `json:"embedding"`
	InputTextTokenCount int       `json:"inputTextTokenCount"`
}

// Embed embeds the given texts using Titan Text Embeddings V2 via InvokeModel.
// Bedrock embeds one text at a time, so this loops over texts.
// Sparse vectors are not supported by Titan; EmbedResult.Sparse will always be nil.
func (c *BedrockClient) Embed(ctx context.Context, texts []string) ([]EmbedResult, error) {
	results := make([]EmbedResult, 0, len(texts))
	for i, text := range texts {
		body, err := json.Marshal(titanEmbedRequest{
			InputText:  text,
			Dimensions: 1024,
			Normalize:  true,
		})
		if err != nil {
			return nil, fmt.Errorf("llm/bedrock: marshal embed request for text %d: %w", i, err)
		}

		output, err := c.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(c.cfg.EmbedModelID),
			ContentType: aws.String("application/json"),
			Body:        body,
		})
		if err != nil {
			return nil, fmt.Errorf("llm/bedrock: invoke embed model for text %d: %w", i, err)
		}

		var resp titanEmbedResponse
		if err := json.Unmarshal(output.Body, &resp); err != nil {
			return nil, fmt.Errorf("llm/bedrock: unmarshal embed response for text %d: %w", i, err)
		}

		results = append(results, EmbedResult{
			Dense:  resp.Embedding,
			Sparse: nil,
		})
	}
	return results, nil
}

// GenerateText generates text for the given prompt using the Converse API.
func (c *BedrockClient) GenerateText(ctx context.Context, params GenerateTextParams) (string, error) {
	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String(c.cfg.ChatModelID),
		Messages: []types.Message{
			{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberText{Value: params.Prompt},
				},
			},
		},
	}

	if params.MaxTokens > 0 || params.Temperature != 0 || len(params.StopSequences) > 0 {
		inferenceConfig := &types.InferenceConfiguration{}
		if params.MaxTokens > 0 {
			maxTokens := int32(params.MaxTokens)
			inferenceConfig.MaxTokens = &maxTokens
		}
		if params.Temperature != 0 {
			temp := float32(params.Temperature)
			inferenceConfig.Temperature = &temp
		}
		if len(params.StopSequences) > 0 {
			inferenceConfig.StopSequences = params.StopSequences
		}
		input.InferenceConfig = inferenceConfig
	}

	response, err := c.client.Converse(ctx, input)
	if err != nil {
		return "", fmt.Errorf("llm/bedrock: converse for generate text: %w", err)
	}

	text, err := extractConverseText(response)
	if err != nil {
		return "", fmt.Errorf("llm/bedrock: extract generate text response: %w", err)
	}
	return text, nil
}

// Chat runs a multi-turn conversation using the Converse API.
// System messages are extracted and passed separately; user/assistant messages form the conversation.
func (c *BedrockClient) Chat(ctx context.Context, params ChatParams) (ChatMessage, error) {
	var systemPrompts []types.SystemContentBlock
	var messages []types.Message

	for _, msg := range params.Messages {
		switch msg.Role {
		case RoleSystem:
			systemPrompts = append(systemPrompts, &types.SystemContentBlockMemberText{Value: msg.Content})
		case RoleUser:
			messages = append(messages, types.Message{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberText{Value: msg.Content},
				},
			})
		case RoleAssistant:
			messages = append(messages, types.Message{
				Role: types.ConversationRoleAssistant,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberText{Value: msg.Content},
				},
			})
		}
	}

	input := &bedrockruntime.ConverseInput{
		ModelId:  aws.String(c.cfg.ChatModelID),
		Messages: messages,
	}

	if len(systemPrompts) > 0 {
		input.System = systemPrompts
	}

	if params.MaxTokens > 0 || params.Temperature != 0 {
		inferenceConfig := &types.InferenceConfiguration{}
		if params.MaxTokens > 0 {
			maxTokens := int32(params.MaxTokens)
			inferenceConfig.MaxTokens = &maxTokens
		}
		if params.Temperature != 0 {
			temp := float32(params.Temperature)
			inferenceConfig.Temperature = &temp
		}
		input.InferenceConfig = inferenceConfig
	}

	response, err := c.client.Converse(ctx, input)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("llm/bedrock: converse for chat: %w", err)
	}

	text, err := extractConverseText(response)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("llm/bedrock: extract chat response: %w", err)
	}

	return ChatMessage{
		Role:    RoleAssistant,
		Content: text,
	}, nil
}

// GenerateStructured generates structured JSON output using the Converse API.
// The prompt should instruct the model to return JSON matching the provided schema.
func (c *BedrockClient) GenerateStructured(
	ctx context.Context,
	params GenerateStructuredParams,
) (json.RawMessage, error) {
	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String(c.cfg.ChatModelID),
		Messages: []types.Message{
			{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberText{Value: params.Prompt},
				},
			},
		},
	}

	if params.MaxTokens > 0 || params.Temperature != 0 {
		inferenceConfig := &types.InferenceConfiguration{}
		if params.MaxTokens > 0 {
			maxTokens := int32(params.MaxTokens)
			inferenceConfig.MaxTokens = &maxTokens
		}
		if params.Temperature != 0 {
			temp := float32(params.Temperature)
			inferenceConfig.Temperature = &temp
		}
		input.InferenceConfig = inferenceConfig
	}

	response, err := c.client.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("llm/bedrock: converse for structured output: %w", err)
	}

	text, err := extractConverseText(response)
	if err != nil {
		return nil, fmt.Errorf("llm/bedrock: extract structured output response: %w", err)
	}

	return json.RawMessage(text), nil
}

// extractConverseText extracts the text content from a Converse API response.
func extractConverseText(response *bedrockruntime.ConverseOutput) (string, error) {
	outputMsg, ok := response.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return "", fmt.Errorf("unexpected output type: %T", response.Output)
	}
	if len(outputMsg.Value.Content) == 0 {
		return "", errors.New("empty content in response")
	}
	textBlock, ok := outputMsg.Value.Content[0].(*types.ContentBlockMemberText)
	if !ok {
		return "", fmt.Errorf("unexpected content block type: %T", outputMsg.Value.Content[0])
	}
	return textBlock.Value, nil
}
