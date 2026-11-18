// Package speech turns an audio or video file into text with a self-hosted
// model.
package speech

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// SampleRate is the rate Gipformer and Silero VAD take.
const SampleRate = 16000

var (
	// ErrUnsupportedMedia means ffmpeg cannot read the file, or it has no audio track.
	ErrUnsupportedMedia = errors.New("ffmpeg cannot read an audio track from the file")
	// ErrTooLong means the audio is longer than the limit.
	ErrTooLong = errors.New("the audio is longer than the limit")
)

// Decode reads the first audio track of the file at path as float32 mono
// samples at 16 kHz. It takes a path because an mp4 with its moov atom at the
// end needs a seekable input. ffmpeg stops one second past maxSeconds, and
// anything longer than maxSeconds is ErrTooLong.
func Decode(ctx context.Context, path string, maxSeconds int) ([]float32, error) {
	stderr := &capped{max: 2048}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error",
		"-i", path, "-map", "a:0", "-vn", "-ac", "1", "-ar", strconv.Itoa(SampleRate),
		"-t", strconv.Itoa(maxSeconds+1), "-f", "f32le", "pipe:1")
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("ffmpeg: %w", ctxErr)
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.Exited() {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedMedia, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	n := len(out) / 4
	if n > maxSeconds*SampleRate {
		return nil, ErrTooLong
	}
	samples := make([]float32, n)
	for i := range samples {
		samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(out[4*i:]))
	}
	return samples, nil
}

// capped keeps the first max bytes written to it. A garbage file makes ffmpeg
// complain at length.
type capped struct {
	bytes.Buffer

	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
