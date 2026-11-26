package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/locale"
)

// maxSimilar is how many of the sample a guide shows.
const maxSimilar = 12

// PriceGuideInput is the draft a guide prices: PriceGuideRequest.
type PriceGuideInput struct {
	Title, CategoryCode, Description, CommuneCode string
	Condition                                     catalogdomain.Condition
	Specs                                         []catalogdomain.Spec
	Photos                                        []uuid.UUID
}

// PriceGuide is UC-LST-01 A3: it checks the account, the category, the
// commune and the photos, draws the sample as the reads do, and answers the
// range, the suggestions, the first similar listings with their distance,
// the sample's prices and the matching watches. A sample that fails gives a
// guide without a comparison; the watchers still answer.
func (a *Assist) PriceGuide(ctx context.Context, account uuid.UUID, in PriceGuideInput) (PriceGuideView, error) {
	started := a.d.Now()
	// E1 · BR-LST-02
	status, err := a.d.Accounts.Status(ctx, account)
	if err != nil {
		return PriceGuideView{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return PriceGuideView{}, r
	}
	cats, err := a.d.Catalog.Categories(ctx, locale.Vietnamese)
	if err != nil {
		return PriceGuideView{}, fmt.Errorf("read categories: %w", err)
	}
	places, err := a.d.Catalog.Places(ctx, []string{in.CommuneCode})
	if err != nil {
		return PriceGuideView{}, fmt.Errorf("read the commune: %w", err)
	}
	place, communeOK := places[in.CommuneCode]
	leaf := slices.ContainsFunc(cats, func(c catalogdomain.Category) bool { return c.Code == in.CategoryCode && c.ParentCode != "" })
	if !leaf || !communeOK {
		e := catalogdomain.ErrInvalidListing
		if !leaf {
			e = e.WithField("category_code", apperr.NotAllowed, "a subcategory")
		}
		if !communeOK {
			e = e.WithField("commune_code", apperr.Unknown, "a commune of the administrative list")
		}
		return PriceGuideView{}, e
	}
	// The photos are checked before any model runs, so a wrong one is
	// invalid_upload and never a guide without a comparison.
	for _, p := range in.Photos {
		if _, err = a.d.Media.QueryUpload(ctx, account, p); err != nil {
			return PriceGuideView{}, fmt.Errorf("check photo %s: %w", p, err)
		}
	}
	origin := catalogdomain.Point{Lat: place.Lat, Lng: place.Lng}
	// The specs read as the listing would store them: keyed, in Vietnamese.
	specs, err := a.d.Catalog.KeySpecs(ctx, in.CategoryCode, in.Specs)
	if err != nil {
		return PriceGuideView{}, fmt.Errorf("key the specs: %w", err)
	}

	v := PriceGuideView{
		Suggestions: []domain.Suggestion{}, Similar: []SimilarView{}, SoldPrices: []int64{}, LivePrices: []int64{},
	}
	samples, err := a.d.draftSample(ctx, a.d.Log, account, catalogdomain.ListingInput{
		Title: in.Title, CategoryCode: in.CategoryCode, Condition: in.Condition, Specs: specs,
		Description: in.Description, Photos: in.Photos,
	}, started)
	if err != nil {
		a.d.Log.WarnContext(ctx, "the price guide goes without a sample", "err", err)
		samples = nil
	}
	ss := samplesOf(samples)
	v.Estimate = domain.MarketEstimate(ss)
	if v.Estimate != nil {
		v.Suggestions = domain.Suggest(ss, *v.Estimate)
	}
	for _, s := range samples {
		if s.Sold {
			v.SoldPrices = append(v.SoldPrices, s.Price)
		} else {
			v.LivePrices = append(v.LivePrices, s.Price)
		}
	}
	slices.Sort(v.SoldPrices)
	slices.Sort(v.LivePrices)
	first := samples[:min(len(samples), maxSimilar)]
	ids := make([]uuid.UUID, len(first))
	for i, s := range first {
		ids[i] = s.ListingID
	}
	summaries, err := a.d.Catalog.Summaries(ctx, ids)
	if err != nil {
		return PriceGuideView{}, fmt.Errorf("read similar listings: %w", err)
	}
	// A seller blocked either way stays out of the list, as out of every
	// search; their prices stay in the range.
	blocked, err := a.d.Accounts.BlockedWith(ctx, account)
	if err != nil {
		return PriceGuideView{}, fmt.Errorf("read blocks: %w", err)
	}
	for _, s := range first {
		sum, ok := summaries[s.ListingID]
		if !ok || slices.Contains(blocked, sum.SellerID) { // deleted since the sample, or blocked
			continue
		}
		sv := SimilarView{
			ListingID: s.ListingID, Title: sum.Title, CoverURL: sum.CoverThumbURL, Price: sum.Price,
			Condition: sum.Condition, Sold: s.Sold, Km: catalogdomain.DistanceKm(origin, s.Point),
		}
		if s.Sold {
			sv.DaysToSell = new(s.DaysToSell)
		}
		v.Similar = append(v.Similar, sv)
	}

	var text strings.Builder
	text.WriteString(in.Title + "\n" + in.Description)
	for _, s := range specs {
		text.WriteString("\n" + s.Label + ": " + s.Value)
	}
	if v.Watchers, err = a.d.Discovery.Watchers(ctx, account, discoverysvc.WatcherQuery{
		CategoryCode: in.CategoryCode, Condition: in.Condition, Point: origin, Text: text.String(),
	}); err != nil {
		return PriceGuideView{}, fmt.Errorf("read watchers: %w", err)
	}

	source := "none"
	if v.Estimate != nil {
		source = string(v.Estimate.Source)
	}
	a.d.Log.InfoContext(ctx, "price guide", "category", in.CategoryCode, "condition", in.Condition,
		"photos", len(in.Photos), "sample", len(samples), "sold", len(v.SoldPrices), "estimate", source,
		"suggestions", len(v.Suggestions), "watchers", len(v.Watchers), "took", a.d.Now().Sub(started))
	return v, nil
}
