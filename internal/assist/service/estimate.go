package service

import (
	"context"
	"fmt"
	"math"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/provider/embed"
)

// marketEstimate is the price range of the listings most like d, or nil:
// without a category, past the deadline, under domain.MinMarketSample
// similar listings, or when the step fails. Both reads call it after their
// draft is saved; d's photos are waiting uploads of account's.
func (d Deps) marketEstimate(ctx restate.Context, read, account uuid.UUID, draft domain.Draft, at, deadline time.Time) *domain.Estimate {
	if !at.Before(deadline) || draft.CategoryCode == "" {
		return nil
	}
	m, err := restate.Run(ctx, func(rc restate.RunContext) (*domain.Estimate, error) {
		text, textErr := d.Catalog.EmbeddingText(rc, catalogdomain.Draft{
			Title: draft.Title, CategoryCode: draft.CategoryCode, Condition: draft.Condition, Specs: draft.Specs,
			Description: draft.Description,
		})
		if textErr != nil {
			return nil, fmt.Errorf("build the embedding text: %w", textErr)
		}
		inputs := make([]string, len(draft.Photos))
		for i, p := range draft.Photos {
			img, imgErr := d.Media.QueryImage(rc, account, p)
			if imgErr != nil {
				return nil, fmt.Errorf("read photo %s: %w", p, imgErr)
			}
			inputs[i] = embed.ImageInput(img)
		}
		q := catalogsvc.PriceQuery{
			Exclude: account, CategoryCode: draft.CategoryCode, Condition: draft.Condition,
			TextMin: d.TextMin, PhotoMin: d.PhotoMin, Now: at,
		}
		embedCtx, cancel := context.WithTimeout(rc, d.CallTimeout)
		defer cancel()
		// A model that fails leaves its list out of the sample.
		if vs, embedErr := d.Text.Embed(embedCtx, []string{text}); embedErr != nil {
			d.Log.WarnContext(rc, "the price sample goes without the text vector", "read", read, "err", embedErr)
		} else {
			q.Text = vs[0]
		}
		if vs, embedErr := d.Image.Embed(embedCtx, inputs); embedErr != nil || len(vs) == 0 {
			d.Log.WarnContext(rc, "the price sample goes without the photo vector", "read", read, "err", embedErr)
		} else {
			// The normalized mean of the photos, as like does with a
			// listing's photos.
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
			if norm > 0 {
				scale := float32(1 / math.Sqrt(norm))
				for j := range mean {
					mean[j] *= scale
				}
				q.Photo = mean
			}
		}
		samples, sampleErr := d.Catalog.PriceSample(rc, q)
		if sampleErr != nil {
			return nil, fmt.Errorf("price sample: %w", sampleErr)
		}
		ss := make([]domain.Sample, len(samples))
		for i, x := range samples {
			ss[i] = domain.Sample{Price: x.Price, Sold: x.Sold}
		}
		return domain.MarketEstimate(ss), nil
	}, restate.WithMaxRetryDuration(deadline.Sub(at)), restate.WithMaxRetryAttempts(modelAttempts))
	if err != nil {
		return nil
	}
	return m
}
