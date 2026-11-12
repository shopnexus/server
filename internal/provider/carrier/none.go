package carrier

import "context"

// None is the carrier of a stack without one, CARRIER=none: production
// until a real carrier's client comes. It makes and reads nothing, and it
// calls nothing off, since it never made a shipment.
type None struct{}

var _ Client = None{}

func (None) Create(context.Context, CreateInput) (Created, error) { return Created{}, ErrUnavailable }

func (None) Lookup(context.Context, Carrier, string) (Parcel, error) { return Parcel{}, ErrUnavailable }

func (None) Cancel(context.Context, Carrier, string) error { return nil }
