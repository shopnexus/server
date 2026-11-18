package embed

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/eliben/go-sentencepiece"
)

// maxTextTokens is the length of the text tower's input: SigLIP 2 was
// trained on 64 ids, padded.
const maxTextTokens = 64

// tokenizer turns text into the text tower's input the way transformers'
// SigLIP 2 processor does with padding="max_length", max_length=64 and
// truncation, on text the caller lowercased (the model was trained on
// lowercased text).
type tokenizer struct {
	sp       *sentencepiece.Processor
	eos, pad int
}

func newTokenizer(path string) (*tokenizer, error) {
	sp, err := sentencepiece.NewProcessorFromPath(path)
	if err != nil {
		return nil, fmt.Errorf("load the tokenizer %s: %w", path, err)
	}
	info := sp.ModelInfo()
	return &tokenizer{sp: sp, eos: info.EndOfSentenceID, pad: info.PadID}, nil
}

// maxTextBytes cuts a text before the tokenizer reads it. The tower takes
// 63 pieces, and no Gemma piece is longer than a few dozen bytes, so 4 KiB
// is far more than those pieces need; the tokenizer allocates in proportion
// to what it reads, and a text of megabytes would cost gigabytes.
const maxTextBytes = 4 << 10

// ids gives the padded input ids and how many come before the padding.
func (t *tokenizer) ids(text string) (ids []int64, n int) {
	if len(text) > maxTextBytes {
		cut := maxTextBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
	}
	toks := t.sp.Encode(strings.ToLower(text))
	raw := make([]int, len(toks))
	for i, tok := range toks {
		raw[i] = tok.ID
	}
	return pack(raw, t.eos, t.pad)
}

// pack keeps at most maxTextTokens-1 ids, appends eos and pads to
// maxTextTokens.
func pack(raw []int, eos, pad int) (ids []int64, n int) {
	if len(raw) > maxTextTokens-1 {
		raw = raw[:maxTextTokens-1]
	}
	ids = make([]int64, maxTextTokens)
	for i, id := range raw {
		ids[i] = int64(id)
	}
	ids[len(raw)] = int64(eos)
	for i := len(raw) + 1; i < maxTextTokens; i++ {
		ids[i] = int64(pad)
	}
	return ids, len(raw) + 1
}
