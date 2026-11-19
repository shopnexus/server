package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Streamer asks a model for plain text and hears it as it is written.
type Streamer interface {
	// Stream sends r without its schema and calls onText with the whole
	// text so far after each piece. It returns the whole text.
	Stream(ctx context.Context, r Request, onText func(string)) (string, error)
}

var _ Streamer = OpenAI{}

func (c OpenAI) Stream(ctx context.Context, r Request, onText func(string)) (string, error) {
	r.SchemaName, r.Schema = "", nil
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
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err = json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", fmt.Errorf("decode chunk: %w", err)
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
