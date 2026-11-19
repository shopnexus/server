package service

import (
	"context"
	"uuid"
)

// RequestCod is UC-DEAL-03: it runs DealRequestCod once per caller and
// idempotencyKey (NFR-IDEM-01), or on every call without a key, then reads
// the deal. The order keeps a copy of the address, so a later edit or
// delete of it leaves the order as it was.
func (s *Deals) RequestCod(ctx context.Context, caller, id, addressID uuid.UUID, idempotencyKey string) (DealView, error) {
	in := CodInput{Caller: caller, DealID: id, AddressID: addressID}
	if _, err := s.requestCod.Call(ctx, invocationKey(caller, idempotencyKey), in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, id)
}
