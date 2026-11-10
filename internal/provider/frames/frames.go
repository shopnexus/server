// Package frames asks ai's POST /video/frames for the sharp, distinct frames
// of a video. It knows nothing about what the frames are for.
package frames

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
	// ErrUnreadable is a file ai cannot read as a video, or one with no
	// video stream.
	ErrUnreadable = errors.New("frames: ai cannot read the video")
	ErrTooShort   = errors.New("frames: the video is too short")
	ErrTooLong    = errors.New("frames: the video is too long")
	// ErrRefused is any other refusal of the request itself; sending it
	// again will not help. A 429 or a 5xx is another, plain, error.
	ErrRefused = errors.New("frames: ai refused the request")
)

// Client calls ai.
type Client struct {
	BaseURL string // ends in /v1
	HTTP    *http.Client
}

// Frame is one kept frame, a JPEG.
type Frame struct {
	AtSeconds float64 `json:"at_seconds"`
	Sharpness float64 `json:"sharpness"`
	JPEG      []byte  `json:"jpeg"`
}

// Result is ai's answer: the video's length and its kept frames in time
// order.
type Result struct {
	Duration float64 `json:"duration"`
	Frames   []Frame `json:"frames"`
}

// maxReply caps how much of a reply is read: 8 frames of at most 1600 px as
// base64 JPEGs are a few MB.
const maxReply = 32 << 20

// Extract sends size bytes of video to ai and returns its frames.
func (c Client) Extract(ctx context.Context, video io.Reader, size int64) (Result, error) {
	var head bytes.Buffer
	form := multipart.NewWriter(&head)
	if _, err := form.CreateFormFile("file", "video.mp4"); err != nil {
		return Result{}, fmt.Errorf("build form: %w", err)
	}
	tail := "\r\n--" + form.Boundary() + "--\r\n"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/video/frames",
		io.MultiReader(&head, video, strings.NewReader(tail)))
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	req.ContentLength = int64(head.Len()) + size + int64(len(tail))
	req.Header.Set("Content-Type", form.FormDataContentType())
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("call ai frames: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
		var e struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&e)
		switch e.Error.Code {
		case "unsupported_media":
			return Result{}, ErrUnreadable
		case "too_short":
			return Result{}, ErrTooShort
		case "too_long":
			return Result{}, ErrTooLong
		default:
			return Result{}, fmt.Errorf("%w: %d %s", ErrRefused, res.StatusCode, e.Error.Code)
		}
	}
	if res.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("ai frames answered %d", res.StatusCode)
	}
	var out Result
	if err = json.NewDecoder(io.LimitReader(res.Body, maxReply)).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("decode ai frames' reply: %w", err)
	}
	for i, f := range out.Frames {
		if len(f.JPEG) == 0 {
			return Result{}, fmt.Errorf("ai frames gave frame %d without a JPEG", i)
		}
	}
	return out, nil
}
