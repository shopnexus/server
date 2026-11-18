// Package embed turns images and text into SigLIP 2 vectors in one space,
// on CPU, through onnxruntime.
package embed

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	// Dimensions of the vectors.
	Dimensions = 1152
	// ImageTokens is what one image counts in usage: its 24x24 patches.
	ImageTokens = 576

	visionInput, visionOutput = "pixel_values", "pooler_output"
	textInput, textOutput     = "input_ids", "pooler_output"
)

// ErrUnknownModel means the request names a model the service does not have.
var ErrUnknownModel = errors.New("unknown model")

// InputError says which input was refused.
type InputError struct {
	Index int
	Err   error
}

func (e *InputError) Error() string { return fmt.Sprintf("input %d: %v", e.Index, e.Err) }
func (e *InputError) Unwrap() error { return e.Err }

// Config of an Embedder.
type Config struct {
	// Models maps a name to a folder holding vision_model.onnx,
	// text_model.onnx (with its .onnx_data when it has one) and
	// tokenizer.model.
	Models        map[string]string
	NumThreads    int
	MaxConcurrent int
}

// Input is one thing to embed: Image when non-nil, else Text.
type Input struct {
	Image []byte
	Text  string
}

// Result of one Embed call.
type Result struct {
	Vectors [][]float32
	Tokens  int
}

type model struct {
	vision, text *ort.DynamicAdvancedSession
	tok          *tokenizer
}

// Embedder is safe for concurrent use: onnxruntime sessions run
// concurrently, and at most MaxConcurrent calls embed at once.
type Embedder struct {
	models map[string]*model
	slots  chan struct{}
}

// New loads every model. It fails when a file is missing or does not load.
func New(c Config) (*Embedder, error) {
	if err := initORT(); err != nil {
		return nil, err
	}
	e := &Embedder{models: map[string]*model{}, slots: make(chan struct{}, c.MaxConcurrent)}
	for name, dir := range c.Models {
		m, err := load(dir, c.NumThreads)
		if err != nil {
			e.Close()
			return nil, fmt.Errorf("model %s: %w", name, err)
		}
		e.models[name] = m
	}
	return e, nil
}

func load(dir string, threads int) (*model, error) {
	for _, f := range []string{"vision_model.onnx", "text_model.onnx", "tokenizer.model"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return nil, fmt.Errorf("model file: %w", err)
		}
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("session options: %w", err)
	}
	defer func() { _ = opts.Destroy() }()
	if err = opts.SetIntraOpNumThreads(threads); err != nil {
		return nil, fmt.Errorf("set threads: %w", err)
	}
	if err = opts.SetInterOpNumThreads(1); err != nil {
		return nil, fmt.Errorf("set threads: %w", err)
	}
	m := &model{}
	if m.vision, err = ort.NewDynamicAdvancedSession(filepath.Join(dir, "vision_model.onnx"),
		[]string{visionInput}, []string{visionOutput}, opts); err != nil {
		return nil, fmt.Errorf("load vision tower: %w", err)
	}
	if m.text, err = ort.NewDynamicAdvancedSession(filepath.Join(dir, "text_model.onnx"),
		[]string{textInput}, []string{textOutput}, opts); err != nil {
		_ = m.vision.Destroy()
		return nil, fmt.Errorf("load text tower: %w", err)
	}
	if m.tok, err = newTokenizer(filepath.Join(dir, "tokenizer.model")); err != nil {
		_ = m.vision.Destroy()
		_ = m.text.Destroy()
		return nil, err
	}
	return m, nil
}

// HasModel says whether name is loaded.
func (e *Embedder) HasModel(name string) bool { _, ok := e.models[name]; return ok }

// Models lists the loaded names, sorted.
func (e *Embedder) Models() []string {
	names := make([]string, 0, len(e.models))
	for n := range e.models {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Embed embeds every input of one request: the images in one run of the
// vision tower, the texts in one run of the text tower. It waits for a slot
// first. A refused image is an *InputError wrapping ErrBadImage, before any
// run.
func (e *Embedder) Embed(ctx context.Context, name string, in []Input) (Result, error) {
	m, ok := e.models[name]
	if !ok {
		return Result{}, ErrUnknownModel
	}
	// Decoding and tokenizing run under the slot too, so the slots bound the
	// memory a request holds, and a caller gone while it waited starts
	// nothing.
	select {
	case e.slots <- struct{}{}:
	case <-ctx.Done():
		return Result{}, fmt.Errorf("wait for a slot: %w", ctx.Err())
	}
	defer func() { <-e.slots }()
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("wait for a slot: %w", err)
	}
	var (
		imgIdx, txtIdx []int
		pix            []float32
		ids            []int64
		res            = Result{Vectors: make([][]float32, len(in))}
	)
	for i, x := range in {
		if x.Image != nil {
			p, err := pixels(x.Image)
			if err != nil {
				return Result{}, &InputError{Index: i, Err: err}
			}
			pix = append(pix, p...)
			imgIdx = append(imgIdx, i)
			res.Tokens += ImageTokens
			continue
		}
		t, n := m.tok.ids(x.Text)
		ids = append(ids, t...)
		txtIdx = append(txtIdx, i)
		res.Tokens += n
	}

	if len(imgIdx) > 0 {
		in, err := ort.NewTensor(ort.NewShape(int64(len(imgIdx)), 3, imageSide, imageSide), pix)
		if err != nil {
			return Result{}, fmt.Errorf("image tensor: %w", err)
		}
		vecs, err := run(m.vision, in, len(imgIdx))
		if err != nil {
			return Result{}, fmt.Errorf("vision tower: %w", err)
		}
		for k, i := range imgIdx {
			res.Vectors[i] = vecs[k]
		}
	}
	if len(txtIdx) > 0 {
		in, err := ort.NewTensor(ort.NewShape(int64(len(txtIdx)), maxTextTokens), ids)
		if err != nil {
			return Result{}, fmt.Errorf("text tensor: %w", err)
		}
		vecs, err := run(m.text, in, len(txtIdx))
		if err != nil {
			return Result{}, fmt.Errorf("text tower: %w", err)
		}
		for k, i := range txtIdx {
			res.Vectors[i] = vecs[k]
		}
	}
	return res, nil
}

// run runs one tower on a batch and returns n normalized vectors. It takes
// ownership of in.
func run[T ort.TensorData](s *ort.DynamicAdvancedSession, in *ort.Tensor[T], n int) ([][]float32, error) {
	defer func() { _ = in.Destroy() }()
	outs := []ort.Value{nil}
	if err := s.Run([]ort.Value{in}, outs); err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	defer func() { _ = outs[0].Destroy() }()
	out, ok := outs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("output is %T, not float32", outs[0])
	}
	data := out.GetData()
	if len(data) != n*Dimensions {
		return nil, fmt.Errorf("output holds %d floats, want %d", len(data), n*Dimensions)
	}
	vecs := make([][]float32, n)
	for k := range n {
		v := slices.Clone(data[k*Dimensions : (k+1)*Dimensions])
		normalize(v)
		vecs[k] = v
	}
	return vecs, nil
}

func normalize(v []float32) {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= inv
	}
}

// Close releases every session.
func (e *Embedder) Close() {
	for _, m := range e.models {
		_ = m.vision.Destroy()
		_ = m.text.Destroy()
	}
}
