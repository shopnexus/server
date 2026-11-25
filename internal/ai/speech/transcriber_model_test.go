// These tests load the real model and skip without it. Run them from the
// repo root with
//
//	AI_TEST_MODELS=$PWD/tmp/models AI_TEST_WAVS=$PWD/tmp/testwavs go test -race ./internal/ai/...
//
// after scripts/fetch-models.sh has filled the models (AI_TEST_MODELS
// defaults to /models) and the clips of sherpa-onnx-zipformer-vi-2025-04-20
// are in the second folder. go test runs in the package's folder, so the
// paths must be absolute.

package speech

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/ai/aitest"
)

func newTranscriber(t *testing.T, maxConcurrent, maxSeconds int) *Transcriber {
	t.Helper()
	models := aitest.ModelsDir(t, "gipformer-1.5")
	tr, err := New(Config{
		Dir:             filepath.Join(models, "gipformer-1.5"),
		VADModel:        filepath.Join(models, "silero_vad.onnx"),
		NumThreads:      2,
		MaxConcurrent:   maxConcurrent,
		MaxAudioSeconds: maxSeconds,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	return tr
}

// pieces is how Transcribe splits the file at path: the pieces VAD found,
// and the seconds of audio decoded and of speech in the pieces.
func pieces(t *testing.T, tr *Transcriber, path string) (n int, audio, speech float64) {
	t.Helper()
	samples, err := Decode(t.Context(), path, tr.maxSeconds)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := tr.split(samples)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		speech += float64(len(p)) / SampleRate
	}
	return len(ps), float64(len(samples)) / SampleRate, speech
}

func wav(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(aitest.WavsDir(t), name)
}

// What base Gipformer 1.5 heard in the three clips on 2026-11-09:
//
//	0.wav  rồi cũng hỗ trợ cho lâu lâu cũng cho gạo cho này kia
//	1.wav  những nơi đã khống chế được căn bệnh
//	2.wav  âm lượng tivi giảm
//
//nolint:dupword // "lâu lâu" above is one Vietnamese word
var keywords = map[string][]string{
	"0.wav": {"hỗ trợ", "gạo"},
	"1.wav": {"khống chế", "căn bệnh"},
	"2.wav": {"âm lượng", "tivi"},
}

func hasAll(t *testing.T, text string, words []string) {
	t.Helper()
	for _, w := range words {
		if !strings.Contains(text, w) {
			t.Errorf("text %q lacks %q", text, w)
		}
	}
}

// long writes the three clips twice and 0.wav and 1.wav once more, with 1.5 s
// of silence after each, about 32 s in all, as an mp4 with a black picture.
func long(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "long.mp4")
	order := []string{"0.wav", "1.wav", "2.wav", "0.wav", "1.wav", "2.wav", "0.wav", "1.wav"}
	args := make([]string, 0, 4+2*len(order)+22)
	args = append(args, "-nostdin", "-v", "error", "-y")
	for _, f := range order {
		args = append(args, "-i", wav(t, f))
	}
	args = append(args, "-f", "lavfi", "-t", "1.5", "-i", "anullsrc=r=16000:cl=mono",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:r=10")
	silence := len(order)
	var filter strings.Builder
	for i := range order {
		filter.WriteString("[" + strconv.Itoa(i) + "][" + strconv.Itoa(silence) + "]")
	}
	filter.WriteString("concat=n=" + strconv.Itoa(2*len(order)) + ":v=0:a=1[a]")
	args = append(args, "-filter_complex", filter.String(), "-map", "[a]", "-map", strconv.Itoa(silence+1)+":v",
		"-shortest", "-c:v", "mpeg4", "-c:a", "aac", out)
	if b, err := exec.CommandContext(t.Context(), "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, b)
	}
	return out
}

func TestTranscribeEachClip(t *testing.T) { //nolint:paralleltest // loads the model; tests in parallel would each hold a copy
	tr := newTranscriber(t, 1, 300)
	for name, words := range keywords { //nolint:paralleltest // one transcriber, one slot
		t.Run(name, func(t *testing.T) {
			text, err := tr.Transcribe(context.Background(), wav(t, name))
			if err != nil {
				t.Fatal(err)
			}
			if text != strings.ToLower(text) {
				t.Errorf("text %q is not lower case", text)
			}
			hasAll(t, text, words)
			if n, _, _ := pieces(t, tr, wav(t, name)); n != 1 {
				t.Errorf("pieces = %d, want 1", n)
			}
		})
	}
}

func TestLongVideoIsSplitAtSilences(t *testing.T) { //nolint:paralleltest // loads the model; tests in parallel would each hold a copy
	tr := newTranscriber(t, 1, 300)
	f := long(t)
	text, err := tr.Transcribe(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	n, audio, speech := pieces(t, tr, f)
	if n < 6 {
		t.Errorf("pieces = %d, want at least 6", n)
	}
	if audio < 30 || audio > 34 {
		t.Errorf("audio seconds = %v, want about 32", audio)
	}
	if speech <= 0 || speech >= audio {
		t.Errorf("speech seconds = %v of %v", speech, audio)
	}
	for _, words := range keywords {
		hasAll(t, text, words)
	}
}

func TestSilenceGivesEmptyText(t *testing.T) { //nolint:paralleltest // loads the model; tests in parallel would each hold a copy
	tr := newTranscriber(t, 1, 300)
	f := filepath.Join(t.TempDir(), "silence.mp4")
	if b, err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono:d=3", "-c:a", "aac", f).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, b)
	}
	text, err := tr.Transcribe(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if n, _, _ := pieces(t, tr, f); text != "" || n != 0 {
		t.Errorf("text %q of %d pieces, want no text and no pieces", text, n)
	}
}

func TestTooLong(t *testing.T) { //nolint:paralleltest // loads the model; tests in parallel would each hold a copy
	tr := newTranscriber(t, 1, 10)
	if _, err := tr.Transcribe(context.Background(), long(t)); !errors.Is(err, ErrTooLong) {
		t.Errorf("err = %v, want ErrTooLong", err)
	}
}

// A request waits for a free slot until its client gives up.
func TestWaitsForASlot(t *testing.T) { //nolint:paralleltest // loads the model; tests in parallel would each hold a copy
	tr := newTranscriber(t, 1, 300)
	tr.slots <- struct{}{} // the only slot is taken
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := tr.Transcribe(ctx, wav(t, "0.wav")); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	<-tr.slots
}

// One recognizer serves every request. Run with -race.
func TestConcurrentRequestsAgree(t *testing.T) { //nolint:paralleltest // loads the model; tests in parallel would each hold a copy
	tr := newTranscriber(t, 4, 300)
	f := long(t)
	want, err := tr.Transcribe(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 2 {
				got, err := tr.Transcribe(context.Background(), f)
				if err != nil {
					t.Error(err)
					return
				}
				if got != want {
					t.Errorf("concurrent text %q, want %q", got, want)
				}
			}
		})
	}
	wg.Wait()
}
