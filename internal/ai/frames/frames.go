// Package frames cuts a video into its sharpest distinct frames, as JPEGs
// for the server to keep as listing photos.
package frames

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/shopnexus/server/internal/ai/domain"
)

// MinSeconds is the shortest video the app takes.
const MinSeconds = 3.0

const (
	perSecond   = 2    // frames decoded per second of video
	longSide    = 1600 // a frame's long side at most, never scaled up
	keep        = 8    // frames kept at most, one per equal span of the video
	dupDistance = 6    // dHash bits within which two frames are duplicates
	jpegQuality = 90
	scoreSide   = 512 // long side of the grey copy a frame is scored on
)

var (
	// ErrNoVideo means ffprobe or ffmpeg cannot read a video stream with a
	// duration from the file.
	ErrNoVideo = errors.New("no video stream ffmpeg can read")
	// ErrTooShort means the video is shorter than Config.MinSeconds.
	ErrTooShort = errors.New("the video is shorter than the limit")
	// ErrTooLong means the video is longer than Config.MaxSeconds.
	ErrTooLong = errors.New("the video is longer than the limit")
)

// Config bounds a video and sets the blur floor.
type Config struct {
	MinSeconds, MaxSeconds float64
	// MinSharpness is the Laplacian variance under which a frame counts as
	// blurred.
	MinSharpness float64
}

// Extract reads the video at path and returns its duration and the kept
// frames in time order; the duration is set on an error past the probe. It
// takes a path because an mp4 with its moov atom at the end needs a
// seekable input.
func Extract(ctx context.Context, path string, c Config) (domain.FramesOutput, error) {
	duration, err := probe(ctx, path)
	if err != nil {
		return domain.FramesOutput{}, err
	}
	switch {
	case duration < c.MinSeconds:
		return domain.FramesOutput{Duration: duration}, ErrTooShort
	case duration > c.MaxSeconds:
		return domain.FramesOutput{Duration: duration}, ErrTooLong
	}
	sel := newSelector(duration, c.MinSharpness)
	if err := decode(ctx, path, c.MaxSeconds, sel.offer); err != nil {
		return domain.FramesOutput{Duration: duration}, err
	}
	kept := sel.pick()
	res := domain.FramesOutput{Duration: duration, Frames: make([]domain.Frame, 0, len(kept))}
	for _, k := range kept {
		b, err := encodeJPEG(k)
		if err != nil {
			return domain.FramesOutput{Duration: duration}, fmt.Errorf("encode frame: %w", err)
		}
		res.Frames = append(res.Frames, domain.Frame{AtSeconds: k.at, Sharpness: k.sharpness, JPEG: b})
	}
	return res, nil
}

type probeStream struct {
	CodecType string `json:"codec_type"`
}

// probe returns the container's duration in seconds. A file without a video
// stream or a duration, such as audio or a photo, is ErrNoVideo.
func probe(ctx context.Context, path string) (float64, error) {
	stderr := &capped{max: 2048}
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", "format=duration:stream=codec_type", "-of", "json", path)
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, fmt.Errorf("ffprobe: %w", ctxErr)
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.Exited() {
			return 0, fmt.Errorf("%w: %s", ErrNoVideo, strings.TrimSpace(stderr.String()))
		}
		return 0, fmt.Errorf("ffprobe: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var p struct {
		Streams []probeStream `json:"streams"`
		Format  struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err = json.Unmarshal(out, &p); err != nil {
		return 0, fmt.Errorf("read ffprobe output: %w", err)
	}
	if !slices.ContainsFunc(p.Streams, func(s probeStream) bool { return s.CodecType == "video" }) {
		return 0, fmt.Errorf("%w: the file has no video stream", ErrNoVideo)
	}
	d, err := strconv.ParseFloat(p.Format.Duration, 64)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%w: the file has no duration", ErrNoVideo)
	}
	return d, nil
}

// decode runs ffmpeg over the first video stream, upright (ffmpeg applies
// the display matrix by default), perSecond frames a second, at most
// longSide px on the long side and never scaled up, and hands each frame to
// each with its scores. PPM frames carry their own size.
func decode(ctx context.Context, path string, maxSeconds float64, each func(*candidate)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	scale := fmt.Sprintf("scale=w='if(gt(iw,ih),min(%[1]d,iw),-2)':h='if(gt(iw,ih),-2,min(%[1]d,ih))':flags=area", longSide)
	stderr := &capped{max: 2048}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", path,
		"-map", "0:v:0", "-an", "-t", strconv.FormatFloat(maxSeconds+1, 'f', -1, 64),
		"-vf", fmt.Sprintf("fps=%d,%s", perSecond, scale),
		"-pix_fmt", "rgb24", "-f", "image2pipe", "-c:v", "ppm", "pipe:1")
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	r := bufio.NewReaderSize(out, 1<<20)
	var g []float64
	var readErr error
	for i := 0; ; i++ {
		rgb, w, h, err := readPPM(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			readErr = err
			cancel()
			break
		}
		g = grey(rgb, g)
		tw, th := scoreSize(w, h)
		small := shrink(g, w, h, tw, th)
		each(&candidate{
			at: float64(i) / perSecond, sharpness: sharpness(small, tw, th), hash: dhash(small, tw, th),
			rgb: rgb, w: w, h: h,
		})
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return fmt.Errorf("read ffmpeg frames: %w: %s", readErr, strings.TrimSpace(stderr.String()))
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	if waitErr != nil {
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) && exit.Exited() {
			return fmt.Errorf("%w: %s", ErrNoVideo, strings.TrimSpace(stderr.String()))
		}
		return fmt.Errorf("ffmpeg: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// readPPM reads one binary PPM frame of w×h. A clean end of the stream is
// [io.EOF].
func readPPM(r *bufio.Reader) (rgb []byte, w, h int, err error) {
	var magic string
	var maxval int
	if _, err = fmt.Fscan(r, &magic, &w, &h, &maxval); err != nil {
		return nil, 0, 0, fmt.Errorf("frame header: %w", err)
	}
	if magic != "P6" || maxval != 255 || w < 1 || h < 1 || w > 8192 || h > 8192 {
		return nil, 0, 0, fmt.Errorf("unexpected frame header %q %d×%d max %d", magic, w, h, maxval)
	}
	// One whitespace byte separates the header from the pixels.
	if _, err = r.ReadByte(); err != nil {
		return nil, 0, 0, io.ErrUnexpectedEOF
	}
	rgb = make([]byte, w*h*3)
	if _, err = io.ReadFull(r, rgb); err != nil {
		return nil, 0, 0, io.ErrUnexpectedEOF
	}
	return rgb, w, h, nil
}

// candidate is a decoded frame with its scores.
type candidate struct {
	at, sharpness float64
	hash          uint64
	rgb           []byte
	w, h          int
}

// selector keeps, for each of keep equal spans of the video, its sharpest
// frame at or above the floor, so at most keep full frames stay in memory,
// plus the sharpest frame overall.
type selector struct {
	spanSeconds  float64
	minSharpness float64
	spans        [keep]*candidate
	sharpest     *candidate
}

func newSelector(duration, minSharpness float64) *selector {
	return &selector{spanSeconds: duration / keep, minSharpness: minSharpness}
}

func (s *selector) offer(c *candidate) {
	if s.sharpest == nil || c.sharpness > s.sharpest.sharpness {
		s.sharpest = c
	}
	if c.sharpness < s.minSharpness {
		return
	}
	i := min(int(c.at/s.spanSeconds), keep-1)
	if s.spans[i] == nil || c.sharpness > s.spans[i].sharpness {
		s.spans[i] = c
	}
}

// pick returns the frames to keep in time order, dropping a span winner
// that is a near-duplicate of a sharper one. When every frame is under the
// floor it keeps the sharpest frame: a plain item on a plain table scores
// low everywhere.
func (s *selector) pick() (kept []*candidate) {
	var winners []*candidate
	for _, c := range s.spans {
		if c != nil {
			winners = append(winners, c)
		}
	}
	if len(winners) == 0 {
		if s.sharpest == nil {
			return nil
		}
		return []*candidate{s.sharpest}
	}
	slices.SortFunc(winners, func(a, b *candidate) int { return cmp.Compare(b.sharpness, a.sharpness) })
next:
	for _, c := range winners {
		for _, k := range kept {
			if distance(c.hash, k.hash) <= dupDistance {
				continue next
			}
		}
		kept = append(kept, c)
	}
	slices.SortFunc(kept, func(a, b *candidate) int { return cmp.Compare(a.at, b.at) })
	return kept
}

func encodeJPEG(c *candidate) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	for i, j := 0, 0; i < len(c.rgb); i, j = i+3, j+4 {
		img.Pix[j], img.Pix[j+1], img.Pix[j+2], img.Pix[j+3] = c.rgb[i], c.rgb[i+1], c.rgb[i+2], 0xff
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return b.Bytes(), nil
}

// capped keeps the first max bytes written to it. A garbage file makes
// ffmpeg complain at length.
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
