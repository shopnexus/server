package service

import (
	"context"
	"uuid"
)

// RequestCod is UC-DEAL-03: it runs DealRequestCod, then reads the deal.
// The order keeps a copy of the address, so a later edit or delete of it
// leaves the order as it was.
func (s *Deals) RequestCod(ctx context.Context, caller, id uuid.UUID, price int64, addressID, key uuid.UUID) (DealView, error) {
	in := CodInput{Caller: caller, DealID: id, Price: price, AddressID: addressID, Key: key}
	if _, err := s.requestCod.Call(ctx, "", in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, id)
}
