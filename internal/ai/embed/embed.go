// Package embed turns images and text into SigLIP 2 vectors in one space,
// on CPU, through onnxruntime.
package embed

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/shopnexus/server/internal/ai/domain"
)

const (
	// Dimensions of the vectors.
	Dimensions = 1152

	visionInput, visionOutput = "pixel_values", "pooler_output"
	textInput, textOutput     = "input_ids", "pooler_output"
)

// InputError says which input was refused.
type InputError struct {
	Index int
	Err   error
}

func (e *InputError) Error() string { return fmt.Sprintf("input %d: %v", e.Index, e.Err) }
func (e *InputError) Unwrap() error { return e.Err }

// Config of an Embedder.
type Config struct {
	// Dir holds vision_model.onnx, text_model.onnx (with its .onnx_data
	// when it has one) and tokenizer.model.
	Dir           string
	NumThreads    int
	MaxConcurrent int
}

// Input is one thing to embed: Image when non-nil, else Text.
type Input = domain.EmbedItem

type model struct {
	vision, text *ort.DynamicAdvancedSession
	tok          *tokenizer
}

// Embedder is safe for concurrent use: onnxruntime sessions run
// concurrently, and at most MaxConcurrent calls embed at once.
type Embedder struct {
	m     *model
	slots chan struct{}
}

// New loads the model. It fails when a file is missing or does not load.
func New(c Config) (*Embedder, error) {
	if err := initORT(); err != nil {
		return nil, err
	}
	m, err := load(c.Dir, c.NumThreads)
	if err != nil {
		return nil, err
	}
	return &Embedder{m: m, slots: make(chan struct{}, c.MaxConcurrent)}, nil
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

// Embed embeds every input of one request, a vector each in input order:
// the images in one run of the vision tower, the texts in one run of the
// text tower. It waits for a slot first. A refused image is an
// *InputError wrapping ErrBadImage, before any run.
func (e *Embedder) Embed(ctx context.Context, in []Input) ([][]float32, error) {
	m := e.m
	// Decoding and tokenizing run under the slot too, so the slots bound the
	// memory a request holds, and a caller gone while it waited starts
	// nothing.
	select {
	case e.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for a slot: %w", ctx.Err())
	}
	defer func() { <-e.slots }()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("wait for a slot: %w", err)
	}
	var (
		imgIdx, txtIdx []int
		pix            []float32
		ids            []int64
		vectors        = make([][]float32, len(in))
	)
	for i, x := range in {
		if x.Image != nil {
			p, err := pixels(x.Image)
			if err != nil {
				return nil, &InputError{Index: i, Err: err}
			}
			pix = append(pix, p...)
			imgIdx = append(imgIdx, i)
			continue
		}
		t, _ := m.tok.ids(x.Text)
		ids = append(ids, t...)
		txtIdx = append(txtIdx, i)
	}

	if len(imgIdx) > 0 {
		in, err := ort.NewTensor(ort.NewShape(int64(len(imgIdx)), 3, imageSide, imageSide), pix)
		if err != nil {
			return nil, fmt.Errorf("image tensor: %w", err)
		}
		vecs, err := run(m.vision, in, len(imgIdx))
		if err != nil {
			return nil, fmt.Errorf("vision tower: %w", err)
		}
		for k, i := range imgIdx {
			vectors[i] = vecs[k]
		}
	}
	if len(txtIdx) > 0 {
		in, err := ort.NewTensor(ort.NewShape(int64(len(txtIdx)), maxTextTokens), ids)
		if err != nil {
			return nil, fmt.Errorf("text tensor: %w", err)
		}
		vecs, err := run(m.text, in, len(txtIdx))
		if err != nil {
			return nil, fmt.Errorf("text tower: %w", err)
		}
		for k, i := range txtIdx {
			vectors[i] = vecs[k]
		}
	}
	return vectors, nil
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

// Close releases the sessions.
func (e *Embedder) Close() {
	_ = e.m.vision.Destroy()
	_ = e.m.text.Destroy()
}
