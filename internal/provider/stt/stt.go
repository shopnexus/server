// Package stt turns speech into text through an OpenAI-compatible
// POST /audio/transcriptions: LiteLLM's alias stt in the cluster, ai
// locally. It asks for json, whose duration LiteLLM prices the call from;
// a text answer would cost nothing against the key's budget.
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

var (
	// ErrNoAudio is a file with no audio track the endpoint can read.
	ErrNoAudio = errors.New("stt: the file has no audio")
	// ErrRefused is any other refusal of the request itself; sending it
	// again will not help. A 429 or a 5xx is another, plain, error.
	ErrRefused = errors.New("stt: the request was refused")
)

// OpenAI transcribes Vietnamese speech over an OpenAI-compatible API.
type OpenAI struct {
	BaseURL string // ends in /v1
	APIKey  string // empty sends no Authorization header
	Model   string
	HTTP    *http.Client
}

const maxReply = 1 << 20

// Transcribe sends size bytes of audio, or of a video with a sound track,
// and returns what was said; no speech is "".
func (c OpenAI) Transcribe(ctx context.Context, audio io.Reader, size int64) (string, error) {
	var head bytes.Buffer
	form := multipart.NewWriter(&head)
	for _, f := range [][2]string{{"model", c.Model}, {"language", "vi"}, {"response_format", "json"}} {
		if err := form.WriteField(f[0], f[1]); err != nil {
			return "", fmt.Errorf("build form: %w", err)
		}
	}
	if _, err := form.CreateFormFile("file", "video.mp4"); err != nil {
		return "", fmt.Errorf("build form: %w", err)
	}
	tail := "\r\n--" + form.Boundary() + "--\r\n"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/audio/transcriptions",
		io.MultiReader(&head, audio, strings.NewReader(tail)))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.ContentLength = int64(head.Len()) + size + int64(len(tail))
	req.Header.Set("Content-Type", form.FormDataContentType())
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("call %s: %w", c.Model, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
		var e struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&e)
		if e.Error.Code == "unsupported_media" {
			return "", ErrNoAudio
		}
		return "", fmt.Errorf("%w: %s answered %d %s", ErrRefused, c.Model, res.StatusCode, e.Error.Code)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %d", c.Model, res.StatusCode)
	}
	var reply struct {
		Text *string `json:"text"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, maxReply)).Decode(&reply); err != nil {
		return "", fmt.Errorf("decode %s's reply: %w", c.Model, err)
	}
	if reply.Text == nil {
		return "", fmt.Errorf("%s gave no text", c.Model)
	}
	return *reply.Text, nil
}
