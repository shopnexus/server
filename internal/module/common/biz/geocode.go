package commonbiz

import (
	"shopnexus-server/internal/provider/geocoding"
	sharedmodel "shopnexus-server/internal/shared/model"

	restate "github.com/restatedev/sdk-go"
)

type ReverseGeocodeParams struct {
	Latitude  float64 `validate:"required"`
	Longitude float64 `validate:"required"`
}

func (b *CommonHandler) ReverseGeocode(ctx restate.Context, params ReverseGeocodeParams) (geocoding.Result, error) {
	return restate.Run(ctx, func(ctx restate.RunContext) (geocoding.Result, error) {
		result, err := b.geocoder.ReverseGeocode(ctx, params.Latitude, params.Longitude)
		if err != nil {
			return geocoding.Result{}, sharedmodel.WrapErr("reverse geocode", err)
		}
		return result, nil
	})
}

type ForwardGeocodeParams struct {
	Address string `validate:"required"`
}

func (b *CommonHandler) ForwardGeocode(ctx restate.Context, params ForwardGeocodeParams) (geocoding.Result, error) {
	return restate.Run(ctx, func(ctx restate.RunContext) (geocoding.Result, error) {
		result, err := b.geocoder.ForwardGeocode(ctx, params.Address)
		if err != nil {
			return geocoding.Result{}, sharedmodel.WrapErr("forward geocode", err)
		}
		return result, nil
	})
}

type SearchGeocodeParams struct {
	Query string `validate:"required,min=2"`
	Limit int
}

func (b *CommonHandler) SearchGeocode(ctx restate.Context, params SearchGeocodeParams) ([]geocoding.Result, error) {
	return restate.Run(ctx, func(ctx restate.RunContext) ([]geocoding.Result, error) {
		results, err := b.geocoder.Search(ctx, params.Query, params.Limit)
		if err != nil {
			return nil, sharedmodel.WrapErr("search geocode", err)
		}
		return results, nil
	})
}
