package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Streamer asks a model for plain text and hears it as it is written.
type Streamer interface {
	// Stream sends r, with its schema when it has one, and calls onText
	// with the whole text so far after each piece. It returns the whole
	// text. An error the stream sends is an *APIError.
	Stream(ctx context.Context, r Request, onText func(string)) (string, error)
}

var _ Streamer = OpenAI{}

func (c OpenAI) Stream(ctx context.Context, r Request, onText func(string)) (string, error) {
	res, err := c.post(ctx, r, true)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	var text strings.Builder
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), maxReply)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			return text.String(), nil
		}
		var chunk struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err = json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", fmt.Errorf("decode chunk: %w", err)
		}
		if chunk.Error != nil {
			return "", &APIError{Status: http.StatusBadGateway, Message: chunk.Error.Message}
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}
		text.WriteString(chunk.Choices[0].Delta.Content)
		if text.Len() > maxReply {
			return "", fmt.Errorf("reply over %d bytes", maxReply)
		}
		onText(text.String())
	}
	if err = sc.Err(); err != nil {
		return "", fmt.Errorf("read stream: %w", err)
	}
	return "", fmt.Errorf("stream ended without [DONE]")
}
