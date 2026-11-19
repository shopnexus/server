package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/provider/embed"
)

// draftSample draws the price sample of the listings most like in, as of
// at, without account's own: in's embedding text and the normalized mean
// of its photos, which must be waiting uploads of account's. A model that
// fails leaves its list out and logs to log; with neither, the sample is
// nil. The price guide calls it.
func (d Deps) draftSample(ctx context.Context, log *slog.Logger, account uuid.UUID, in catalogdomain.ListingInput, at time.Time,
) ([]catalogsvc.PriceSample, error) {
	text, err := d.Catalog.EmbeddingText(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("build the embedding text: %w", err)
	}
	// An off kind (nil model) reads nothing for it.
	var inputs []string
	for _, p := range in.Photos {
		if d.Image == nil {
			break
		}
		img, imgErr := d.Media.QueryImage(ctx, account, p)
		if imgErr != nil {
			return nil, fmt.Errorf("read photo %s: %w", p, imgErr)
		}
		inputs = append(inputs, embed.ImageInput(img))
	}
	q := catalogsvc.PriceQuery{
		Exclude: account, CategoryCode: in.CategoryCode, Condition: in.Condition,
		TextMin: d.TextMin, PhotoMin: d.PhotoMin, Now: at,
	}
	embedCtx, cancel := context.WithTimeout(ctx, d.CallTimeout)
	defer cancel()
	// A model that fails, or is off, leaves its list out of the sample.
	if d.Text != nil {
		if vs, embedErr := d.Text.Embed(embedCtx, []string{text}); embedErr != nil {
			log.WarnContext(ctx, "the price sample goes without the text vector", "err", embedErr)
		} else {
			q.Text = vs[0]
		}
	}
	if len(inputs) > 0 {
		if vs, embedErr := d.Image.Embed(embedCtx, inputs); embedErr != nil || len(vs) == 0 {
			log.WarnContext(ctx, "the price sample goes without the photo vector", "err", embedErr)
		} else {
			q.Photo = meanVector(vs)
		}
	}
	samples, err := d.Catalog.PriceSample(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("price sample: %w", err)
	}
	return samples, nil
}

// meanVector is the normalized mean of vs, as like does with a listing's
// photos; nil when it is the zero vector.
func meanVector(vs [][]float32) []float32 {
	mean := make([]float32, len(vs[0]))
	for _, v := range vs {
		for j, x := range v {
			mean[j] += x
		}
	}
	var norm float64
	for _, x := range mean {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return nil
	}
	scale := float32(1 / math.Sqrt(norm))
	for j := range mean {
		mean[j] *= scale
	}
	return mean
}

// samplesOf is catalog's sample as the domain weighs it.
func samplesOf(ss []catalogsvc.PriceSample) []domain.Sample {
	out := make([]domain.Sample, len(ss))
	for i, s := range ss {
		out[i] = domain.Sample{Price: s.Price, Sold: s.Sold, DaysToSell: s.DaysToSell}
	}
	return out
}

// guessCategory is the subcategory catalog reads draft's title and
// description as; "" when the guess fails, which leaves the seller to pick.
func (d Deps) guessCategory(ctx context.Context, read uuid.UUID, draft domain.Draft) string {
	code, err := d.Catalog.GuessCategory(ctx, draft.Title, draft.Description)
	if err != nil {
		d.Log.WarnContext(ctx, "the draft goes without a category", "read", read, "err", err)
		return ""
	}
	return code
}
