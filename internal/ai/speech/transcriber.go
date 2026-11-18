package speech

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// ErrUnknownModel means the request names a model the service does not have.
var ErrUnknownModel = errors.New("unknown model")

// Config of a Transcriber.
type Config struct {
	// Models maps a model name to a folder holding encoder.onnx, decoder.onnx,
	// joiner.onnx and tokens.txt of a transducer such as Gipformer.
	Models          map[string]string
	VADModel        string // path of silero_vad.onnx
	NumThreads      int
	MaxConcurrent   int
	MaxAudioSeconds int
}

// Result of one transcription.
type Result struct {
	Text          string
	AudioSeconds  float64 // length of the decoded audio
	SpeechSeconds float64 // length of the pieces VAD found
	Pieces        int
}

// Silero VAD settings. A piece ends at a silence of 0.5 s or at 20 s,
// whichever comes first; Gipformer was trained on clips of that length.
const (
	vadThreshold   = 0.5
	vadMinSilence  = 0.5
	vadMinSpeech   = 0.25
	vadWindow      = 512
	vadMaxSpeech   = 20
	vadBufferInSec = 60
)

// Transcriber turns files into text. It is safe for concurrent use: one
// recognizer per model serves every request, and at most MaxConcurrent
// requests decode at once.
type Transcriber struct {
	recognizers map[string]*sherpa.OfflineRecognizer
	vad         sherpa.VadModelConfig
	maxSeconds  int
	slots       chan struct{}
}

// New loads every model. It fails when a file is missing or a model does not
// load.
func New(c Config) (*Transcriber, error) {
	t := &Transcriber{
		recognizers: map[string]*sherpa.OfflineRecognizer{},
		maxSeconds:  c.MaxAudioSeconds,
		slots:       make(chan struct{}, c.MaxConcurrent),
	}
	t.vad = sherpa.VadModelConfig{SampleRate: SampleRate, NumThreads: 1, Provider: "cpu"}
	t.vad.SileroVad = sherpa.SileroVadModelConfig{
		Model:              c.VADModel,
		Threshold:          vadThreshold,
		MinSilenceDuration: vadMinSilence,
		MinSpeechDuration:  vadMinSpeech,
		WindowSize:         vadWindow,
		MaxSpeechDuration:  vadMaxSpeech,
	}
	if err := exists(c.VADModel); err != nil {
		return nil, err
	}
	probe := sherpa.NewVoiceActivityDetector(&t.vad, vadBufferInSec)
	if probe == nil {
		return nil, fmt.Errorf("cannot load the VAD model %s", c.VADModel)
	}
	sherpa.DeleteVoiceActivityDetector(probe)

	for name, dir := range c.Models {
		files := map[string]string{}
		for _, f := range []string{"encoder.onnx", "decoder.onnx", "joiner.onnx", "tokens.txt"} {
			files[f] = filepath.Join(dir, f)
			if err := exists(files[f]); err != nil {
				t.Close()
				return nil, fmt.Errorf("model %s: %w", name, err)
			}
		}
		cfg := sherpa.OfflineRecognizerConfig{DecodingMethod: "greedy_search"}
		cfg.FeatConfig = sherpa.FeatureConfig{SampleRate: SampleRate, FeatureDim: 80}
		cfg.ModelConfig.Transducer = sherpa.OfflineTransducerModelConfig{
			Encoder: files["encoder.onnx"], Decoder: files["decoder.onnx"], Joiner: files["joiner.onnx"],
		}
		cfg.ModelConfig.Tokens = files["tokens.txt"]
		cfg.ModelConfig.NumThreads = c.NumThreads
		cfg.ModelConfig.Provider = "cpu"
		r := sherpa.NewOfflineRecognizer(&cfg)
		if r == nil {
			t.Close()
			return nil, fmt.Errorf("model %s: cannot load it from %s", name, dir)
		}
		t.recognizers[name] = r
	}
	return t, nil
}

func exists(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("missing %s: %w", path, err)
	}
	return nil
}

// HasModel reports whether name is one of the loaded models.
func (t *Transcriber) HasModel(name string) bool {
	_, ok := t.recognizers[name]
	return ok
}

// Models lists the loaded model names, sorted.
func (t *Transcriber) Models() []string {
	names := make([]string, 0, len(t.recognizers))
	for n := range t.recognizers {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Transcribe decodes the file at path and returns its text in lower case. It
// waits for a free slot while ctx lasts.
func (t *Transcriber) Transcribe(ctx context.Context, model, path string) (Result, error) {
	rec, ok := t.recognizers[model]
	if !ok {
		return Result{}, fmt.Errorf("%w %q", ErrUnknownModel, model)
	}
	select {
	case t.slots <- struct{}{}:
	case <-ctx.Done():
		return Result{}, fmt.Errorf("wait for a slot: %w", ctx.Err())
	}
	defer func() { <-t.slots }()

	samples, err := Decode(ctx, path, t.maxSeconds)
	if err != nil {
		return Result{}, err
	}
	pieces, err := t.split(samples)
	if err != nil {
		return Result{}, err
	}
	res := Result{AudioSeconds: float64(len(samples)) / SampleRate, Pieces: len(pieces)}
	if len(pieces) == 0 {
		return res, nil
	}

	// Each piece decodes alone. On 2026-11-09, two concurrent 288 s requests
	// decoded in batches of 30 s held 2.3 to 2.6 GiB and kept growing; one
	// piece per call held about 1 GiB and took 4.3 s per request against 6.1 s.
	texts := make([]string, len(pieces))
	for i, p := range pieces {
		s := sherpa.NewOfflineStream(rec)
		s.AcceptWaveform(SampleRate, p)
		rec.Decode(s)
		texts[i] = s.GetResult().Text
		sherpa.DeleteOfflineStream(s)
		res.SpeechSeconds += float64(len(p)) / SampleRate
	}
	res.Text = strings.Join(strings.Fields(strings.ToLower(strings.Join(texts, " "))), " ")
	return res, nil
}

// split returns the speech in samples as pieces of at most vadMaxSpeech
// seconds, in order. A VAD keeps state, so every call makes its own.
func (t *Transcriber) split(samples []float32) ([][]float32, error) {
	if len(samples) == 0 {
		return nil, nil
	}
	vad := sherpa.NewVoiceActivityDetector(&t.vad, vadBufferInSec)
	if vad == nil {
		return nil, errors.New("cannot create the VAD")
	}
	defer sherpa.DeleteVoiceActivityDetector(vad)
	var pieces [][]float32
	drain := func() {
		for !vad.IsEmpty() {
			pieces = append(pieces, vad.Front().Samples)
			vad.Pop()
		}
	}
	for i := 0; i < len(samples); i += vadWindow {
		vad.AcceptWaveform(samples[i:min(i+vadWindow, len(samples))])
		drain()
	}
	vad.Flush()
	drain()
	return pieces, nil
}

// Close frees the models.
func (t *Transcriber) Close() {
	for name, r := range t.recognizers {
		sherpa.DeleteOfflineRecognizer(r)
		delete(t.recognizers, name)
	}
}
