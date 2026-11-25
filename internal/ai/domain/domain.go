// Package domain holds what module ai's Restate service ai takes and
// answers, and the errors it ends a call with.
package domain

import (
	"errors"
	"net/http"
)

// ServiceName is the Restate service, after RESTATE_SERVICE_PREFIX.
const ServiceName = "ai"

// The handlers of service ai.
const (
	HandlerTranscribe  = "Transcribe"
	HandlerFrames      = "Frames"
	HandlerEmbedImages = "EmbedImages"
)

// MaxFrames is the most frames Frames answers.
const MaxFrames = 8

// MaxEmbedText is the longest text EmbedImages takes, in characters; the
// text tower reads 64 tokens.
const MaxEmbedText = 300

// TranscribeInput names the video or audio to hear by its object store key.
type TranscribeInput struct {
	Key string `json:"key"`
}

// TranscribeOutput is what was said, in lower case; no speech is "".
type TranscribeOutput struct {
	Text string `json:"text"`
}

// FramesInput names the video to cut by its object store key.
type FramesInput struct {
	Key string `json:"key"`
}

// Frame is one kept frame. JPEG travels as base64.
type Frame struct {
	AtSeconds float64 `json:"at_seconds"`
	Sharpness float64 `json:"sharpness"`
	JPEG      []byte  `json:"jpeg"`
}

// FramesOutput is the video's length in seconds and its kept frames in time
// order.
type FramesOutput struct {
	Duration float64 `json:"duration"`
	Frames   []Frame `json:"frames"`
}

// EmbedImagesInput is up to AI_EMBED_MAX_INPUTS images as data URLs
// (data:image/jpeg;base64,..., or png or webp) or plain texts, which land
// in the same space as the images.
type EmbedImagesInput struct {
	Inputs []string `json:"inputs"`
}

// EmbedImagesOutput holds one unit vector per input, in input order.
type EmbedImagesOutput struct {
	Vectors [][]float32 `json:"vectors"`
}

// EmbedItem is one input of EmbedImages as the model takes it: a decoded
// image when Image is non-nil, else a text.
type EmbedItem struct {
	Image []byte
	Text  string
}

// The errors service ai ends a call with, as terminal errors. Each has its
// own code, so the client gets the same error back.
var (
	// ErrUnreadable is a file ffmpeg cannot read a video stream from, or,
	// for Transcribe, an audio track.
	ErrUnreadable = errors.New("ai: the media cannot be read")
	// ErrNotFound is a key with no object behind it.
	ErrNotFound = errors.New("ai: no object at the key")
	ErrTooShort = errors.New("ai: the media is too short")
	ErrTooLong  = errors.New("ai: the media is too long")
	// ErrRefused is any other refusal of the request itself, such as a bad
	// image, too many inputs or photo vectors off; sending it again will
	// not help.
	ErrRefused = errors.New("ai: the request was refused")
	// ErrFailed is a model or ffmpeg failure. Restate does not retry it;
	// the caller may, as it would a call that timed out.
	ErrFailed = errors.New("ai: the model failed")
)

// codes pairs each error with its terminal error code.
var codes = []struct {
	err  error
	code uint16
}{
	{ErrUnreadable, http.StatusUnsupportedMediaType},
	// 404 is Restate's own answer for a service it does not know.
	{ErrNotFound, http.StatusGone},
	{ErrTooShort, http.StatusUnprocessableEntity},
	{ErrTooLong, http.StatusRequestEntityTooLarge},
	{ErrRefused, http.StatusBadRequest},
	{ErrFailed, http.StatusInternalServerError},
}

// Code is the terminal error code of err, one of the errors above;
// anything else is ErrFailed's.
func Code(err error) uint16 {
	for _, c := range codes {
		if errors.Is(err, c.err) {
			return c.code
		}
	}
	return http.StatusInternalServerError
}

// FromCode is the error a terminal error code stands for, or nil for a
// code service ai does not answer.
func FromCode(code uint16) error {
	for _, c := range codes {
		if c.code == code {
			return c.err
		}
	}
	return nil
}
