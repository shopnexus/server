package frames

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gen runs ffmpeg with args, writing name under t.TempDir(), and returns the
// file's path.
func gen(t *testing.T, name string, args ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.CommandContext(t.Context(), "ffmpeg", append(append([]string{"-nostdin", "-v", "error", "-y"}, args...), out)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, b)
	}
	return out
}

// cfg's floor sits between testsrc2 (about 300 to 650) and testsrc2 under
// gblur sigma 50 (under 10).
var cfg = Config{MinSeconds: MinSeconds, MaxSeconds: 35, MinSharpness: 50}

func size(t *testing.T, b []byte) (w, h int) {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("the frame is not a JPEG: %v", err)
	}
	return img.Bounds().Dx(), img.Bounds().Dy()
}

func TestExtractScalesALandscapeVideo(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.mp4", "-f", "lavfi", "-i", "testsrc2=s=1920x1080:r=10:d=10", "-c:v", "mpeg4", "-q:v", "3")
	res, err := Extract(context.Background(), f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Duration < 9.9 || res.Duration > 10.1 {
		t.Errorf("duration = %v, want 10", res.Duration)
	}
	if res.Decoded != 20 {
		t.Errorf("decoded = %d, want 20 (2 a second)", res.Decoded)
	}
	if n := len(res.Frames); n < 1 || n > keep {
		t.Fatalf("frames = %d, want 1 to %d", n, keep)
	}
	prev := -1.0
	for _, fr := range res.Frames {
		if w, h := size(t, fr.JPEG); w != 1600 || h != 900 {
			t.Errorf("frame at %v s is %d×%d, want 1600×900", fr.AtSeconds, w, h)
		}
		if fr.AtSeconds <= prev {
			t.Errorf("frame at %v s comes after %v s", fr.AtSeconds, prev)
		}
		if fr.Sharpness < cfg.MinSharpness {
			t.Errorf("frame at %v s has sharpness %v, under the floor", fr.AtSeconds, fr.Sharpness)
		}
		prev = fr.AtSeconds
	}
}

func TestExtractNeverScalesUp(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.mp4", "-f", "lavfi", "-i", "testsrc2=s=640x360:r=10:d=4", "-c:v", "mpeg4")
	res, err := Extract(context.Background(), f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) == 0 {
		t.Fatal("no frames")
	}
	if w, h := size(t, res.Frames[0].JPEG); w != 640 || h != 360 {
		t.Errorf("frame is %d×%d, want 640×360", w, h)
	}
}

// A phone films portrait as landscape pixels plus a display matrix.
func TestExtractTurnsAPortraitVideoUpright(t *testing.T) {
	t.Parallel()
	src := gen(t, "s.mp4", "-f", "lavfi", "-i", "testsrc2=s=640x360:r=10:d=4", "-c:v", "mpeg4")
	f := gen(t, "r.mp4", "-display_rotation:v:0", "90", "-i", src, "-c", "copy")
	res, err := Extract(context.Background(), f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) == 0 {
		t.Fatal("no frames")
	}
	if w, h := size(t, res.Frames[0].JPEG); w != 360 || h != 640 {
		t.Errorf("frame is %d×%d, want 360×640", w, h)
	}
}

// An iPhone films HEVC in a .mov with a rotation matrix; Debian's ffmpeg
// decodes it, and the frames come out upright.
func TestExtractReadsAPortraitHEVCMov(t *testing.T) {
	t.Parallel()
	src := gen(t, "h.mov", "-f", "lavfi", "-i", "testsrc2=s=640x360:r=10:d=4", "-c:v", "libx265", "-tag:v", "hvc1", "-x265-params", "log-level=error")
	f := gen(t, "hr.mov", "-display_rotation:v:0", "90", "-i", src, "-c", "copy")
	res, err := Extract(context.Background(), f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) == 0 {
		t.Fatal("no frames")
	}
	if w, h := size(t, res.Frames[0].JPEG); w != 360 || h != 640 {
		t.Errorf("frame is %d×%d, want 360×640", w, h)
	}
}

func TestExtractDropsBlurredFrames(t *testing.T) {
	t.Parallel()
	f := gen(t, "b.mp4", "-f", "lavfi", "-i", "testsrc2=s=640x360:r=10:d=8,gblur=sigma=50:enable='gte(t,4)'", "-c:v", "mpeg4")
	res, err := Extract(context.Background(), f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decoded != 16 || res.Blurred != 8 {
		t.Errorf("decoded %d, blurred %d, want 16 and 8", res.Decoded, res.Blurred)
	}
	if len(res.Frames) == 0 {
		t.Fatal("no frames")
	}
	for _, fr := range res.Frames {
		if fr.AtSeconds >= 4 {
			t.Errorf("kept the blurred frame at %v s", fr.AtSeconds)
		}
	}
}

func TestExtractKeepsOneFrameOfAStillVideo(t *testing.T) {
	t.Parallel()
	png := gen(t, "still.png", "-f", "lavfi", "-i", "testsrc=s=640x360", "-frames:v", "1")
	f := gen(t, "still.mp4", "-loop", "1", "-i", png, "-t", "10", "-r", "10", "-c:v", "mpeg4")
	res, err := Extract(context.Background(), f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 || res.Duplicates != keep-1 {
		t.Errorf("frames %d, duplicates %d, want 1 and %d", len(res.Frames), res.Duplicates, keep-1)
	}
}

// A plain item on a plain table scores low in every frame; the read still
// needs a photo.
func TestExtractKeepsTheSharpestFrameWhenAllAreBlurred(t *testing.T) {
	t.Parallel()
	f := gen(t, "flat.mp4", "-f", "lavfi", "-i", "color=c=gray:s=640x360:r=10:d=4", "-c:v", "mpeg4")
	res, err := Extract(context.Background(), f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 || res.Blurred != res.Decoded {
		t.Errorf("frames %d, blurred %d of %d, want 1 frame and every frame blurred", len(res.Frames), res.Blurred, res.Decoded)
	}
}

func TestExtractRefuses(t *testing.T) {
	t.Parallel()
	garbage := filepath.Join(t.TempDir(), "garbage.mp4")
	if err := os.WriteFile(garbage, []byte("not a video at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		path func() string
		want error
	}{
		{"audio only", func() string {
			return gen(t, "a.m4a", "-f", "lavfi", "-i", "sine=duration=4", "-c:a", "aac")
		}, ErrNoVideo},
		{"garbage", func() string { return garbage }, ErrNoVideo},
		{"a photo", func() string {
			return gen(t, "p.png", "-f", "lavfi", "-i", "testsrc=s=64x64", "-frames:v", "1")
		}, ErrNoVideo},
		{"2 seconds", func() string {
			return gen(t, "short.mp4", "-f", "lavfi", "-i", "testsrc2=s=160x90:r=10:d=2", "-c:v", "mpeg4")
		}, ErrTooShort},
		{"40 seconds", func() string {
			return gen(t, "long.mp4", "-f", "lavfi", "-i", "testsrc2=s=160x90:r=5:d=40", "-c:v", "mpeg4")
		}, ErrTooLong},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := Extract(context.Background(), c.path(), cfg)
			if !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestExtractStopsWithItsContext(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.mp4", "-f", "lavfi", "-i", "testsrc2=s=160x90:r=10:d=4", "-c:v", "mpeg4")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Extract(ctx, f, cfg); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func cand(at, sharp float64, hash uint64) *candidate {
	return &candidate{at: at, sharpness: sharp, hash: hash}
}

// spanHash gives every span a hash 16 bits away from every other span's.
func spanHash(span int) uint64 { return uint64(0xff) << (8 * span) }

func TestPickKeepsTheSharpestFrameOfEachSpan(t *testing.T) {
	t.Parallel()
	s := newSelector(8, 10) // spans of 1 s
	for i := range 16 {
		s.offer(cand(float64(i)/2, float64(100+i%2), spanHash(i/2)))
	}
	kept, dups := s.pick()
	if len(kept) != keep || dups != 0 || s.decoded != 16 || s.blurred != 0 {
		t.Fatalf("kept %d, dups %d, decoded %d, blurred %d", len(kept), dups, s.decoded, s.blurred)
	}
	for i, c := range kept {
		if want := float64(i) + 0.5; c.at != want {
			t.Errorf("kept[%d] at %v s, want %v s (the sharper frame of the span)", i, c.at, want)
		}
	}
}

func TestPickPutsTheLastFrameInTheLastSpan(t *testing.T) {
	t.Parallel()
	s := newSelector(8, 10)
	s.offer(cand(8, 100, 1))
	kept, _ := s.pick()
	if len(kept) != 1 || s.spans[keep-1] == nil {
		t.Errorf("a frame at the very end is lost: kept %d", len(kept))
	}
}

func TestPickDropsBlurredFrames(t *testing.T) {
	t.Parallel()
	s := newSelector(8, 10)
	s.offer(cand(0, 5, spanHash(0)))
	s.offer(cand(1, 50, spanHash(1)))
	s.offer(cand(2, 9.9, spanHash(2)))
	kept, _ := s.pick()
	if len(kept) != 1 || kept[0].at != 1 || s.blurred != 2 {
		t.Errorf("kept %d (first at %v), blurred %d", len(kept), kept[0].at, s.blurred)
	}
}

func TestPickDropsANearDuplicateKeepingTheSharper(t *testing.T) {
	t.Parallel()
	s := newSelector(8, 10)
	s.offer(cand(0, 50, 0b1111_0000))
	s.offer(cand(2, 80, 0b1111_0111)) // 3 bits away
	s.offer(cand(4, 60, spanHash(5)))
	kept, dups := s.pick()
	if dups != 1 || len(kept) != 2 || kept[0].at != 2 || kept[1].at != 4 {
		t.Errorf("kept %v, dups %d; want the frames at 2 s and 4 s in time order", kept, dups)
	}
}

func TestPickKeepsTheSharpestWhenEveryFrameIsBlurred(t *testing.T) {
	t.Parallel()
	s := newSelector(8, 10)
	s.offer(cand(0, 2, 1))
	s.offer(cand(1, 7, 2))
	s.offer(cand(2, 3, 3))
	kept, dups := s.pick()
	if len(kept) != 1 || kept[0].at != 1 || dups != 0 {
		t.Errorf("kept %v, dups %d; want the frame at 1 s", kept, dups)
	}
}

func TestPickWithNoFrame(t *testing.T) {
	t.Parallel()
	if kept, dups := newSelector(8, 10).pick(); kept != nil || dups != 0 {
		t.Errorf("kept %v, dups %d", kept, dups)
	}
}
