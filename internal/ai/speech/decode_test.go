package speech

import (
	"context"
	"errors"
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

// twoSeconds checks got is 2 s of samples, within 2%.
func twoSeconds(t *testing.T, got int) {
	t.Helper()
	want := 2 * SampleRate
	if d := got - want; d < -want/50 || d > want/50 {
		t.Errorf("samples = %d, want %d ±2%%", got, want)
	}
}

func TestDecodeWAV(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.wav", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=2")
	s, err := Decode(context.Background(), f, 10)
	if err != nil {
		t.Fatal(err)
	}
	twoSeconds(t, len(s))
	var peak float32
	for _, v := range s {
		peak = max(peak, v)
	}
	if peak < 0.05 {
		t.Errorf("peak = %v, the tone is missing", peak)
	}
}

func TestDecodeResamplesStereo48k(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.wav", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=2", "-ac", "2")
	s, err := Decode(context.Background(), f, 10)
	if err != nil {
		t.Fatal(err)
	}
	twoSeconds(t, len(s))
}

func TestDecodeM4A(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.m4a", "-f", "lavfi", "-i", "sine=duration=2", "-c:a", "aac")
	s, err := Decode(context.Background(), f, 10)
	if err != nil {
		t.Fatal(err)
	}
	twoSeconds(t, len(s))
}

// ffmpeg writes the moov atom at the end of an mp4 unless told +faststart, as
// many phones do. Reading such a file needs a seekable input.
func TestDecodeMP4WithMoovAtTheEnd(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.mp4",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=2",
		"-f", "lavfi", "-i", "sine=duration=2",
		"-shortest", "-c:v", "mpeg4", "-c:a", "aac")
	s, err := Decode(context.Background(), f, 10)
	if err != nil {
		t.Fatal(err)
	}
	twoSeconds(t, len(s))
}

func TestDecodeMP4WithoutSound(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.mp4", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=2", "-c:v", "mpeg4")
	if _, err := Decode(context.Background(), f, 10); !errors.Is(err, ErrUnsupportedMedia) {
		t.Errorf("err = %v, want ErrUnsupportedMedia", err)
	}
}

func TestDecodeGarbage(t *testing.T) {
	t.Parallel()
	f := filepath.Join(t.TempDir(), "a.mp4")
	if err := os.WriteFile(f, []byte("not a media file at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(context.Background(), f, 10); !errors.Is(err, ErrUnsupportedMedia) {
		t.Errorf("err = %v, want ErrUnsupportedMedia", err)
	}
}

func TestDecodeAcceptsExactlyTheLimit(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.wav", "-f", "lavfi", "-i", "sine=sample_rate=16000:duration=3")
	s, err := Decode(context.Background(), f, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 3*SampleRate {
		t.Errorf("samples = %d, want %d", len(s), 3*SampleRate)
	}
}

// ffmpeg reads one second past the limit and stops, so refusing a long file
// costs no more than the limit.
func TestDecodeStopsAfterTheLimit(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.wav", "-f", "lavfi", "-i", "sine=sample_rate=16000:duration=60")
	if _, err := Decode(context.Background(), f, 3); !errors.Is(err, ErrTooLong) {
		t.Errorf("err = %v, want ErrTooLong", err)
	}
}

func TestDecodeHonoursCancel(t *testing.T) {
	t.Parallel()
	f := gen(t, "a.wav", "-f", "lavfi", "-i", "sine=sample_rate=16000:duration=2")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Decode(ctx, f, 10); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
