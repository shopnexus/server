package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/shopnexus/server/internal/ai/domain"
	"github.com/shopnexus/server/internal/provider/embed"
)

// Photos is the embed.Model of photo vectors: SigLIP 2 in module ai.
// Catalog, discovery and assist take it where they took an embedding API.
type Photos struct {
	AI      Service
	Dims    int           // the length every vector must have
	Timeout time.Duration // one call
}

var _ embed.Model = Photos{}

// maxHalf is the largest magnitude halfvec stores.
const maxHalf = 65504

// Embed takes images as data URLs, as embed.ImageInput writes them, and
// texts as they are, such as a search's look. A refusal is embed.ErrRefused.
func (p Photos) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	vs, err := p.AI.EmbedImages(ctx, inputs)
	if errors.Is(err, domain.ErrRefused) {
		return nil, fmt.Errorf("%w: %w", embed.ErrRefused, err)
	}
	if err != nil {
		return nil, fmt.Errorf("embed photos: %w", err)
	}
	for i, v := range vs {
		if len(v) != p.Dims {
			return nil, fmt.Errorf("ai gave vector %d of %d dimensions, want %d", i, len(v), p.Dims)
		}
		for _, x := range v {
			// halfvec holds ±65504; Postgres would refuse a vector past it.
			if math.IsNaN(float64(x)) || math.Abs(float64(x)) > maxHalf {
				return nil, fmt.Errorf("ai gave a value outside ±%v", maxHalf)
			}
		}
	}
	return vs, nil
}
