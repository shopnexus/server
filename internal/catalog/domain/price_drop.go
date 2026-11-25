package domain

import "time"

// A listing shows its last price change, on cards and its page, while the
// change is a drop of at least PriceDropMinPercent made less than
// PriceDropShownFor ago.
const (
	PriceDropShownFor   = 7 * 24 * time.Hour
	PriceDropMinPercent = 5
)

// PriceDrop is a listing's last price change when it lowered the price.
type PriceDrop struct {
	PreviousPrice int64     `json:"previous_price"`
	DroppedAt     time.Time `json:"dropped_at"`
}

// PriceDrop is the listing's last price change while it shows at now.
func (l *Listing) PriceDrop(now time.Time) (PriceDrop, bool) {
	if l.s.PriceChangedAt == nil || now.Sub(*l.s.PriceChangedAt) >= PriceDropShownFor ||
		(l.s.PreviousPrice-l.s.Price)*100 < l.s.PreviousPrice*PriceDropMinPercent {
		return PriceDrop{}, false
	}
	return PriceDrop{PreviousPrice: l.s.PreviousPrice, DroppedAt: *l.s.PriceChangedAt}, true
}

// repriced records that the price changed from was at now. A raise less
// than PriceDropShownFor old is not a price to drop from, so raising and
// then lowering shows no drop the buyer never had.
func (l *Listing) repriced(was int64, now time.Time) {
	if l.s.PriceChangedAt != nil && now.Sub(*l.s.PriceChangedAt) < PriceDropShownFor && l.s.PreviousPrice < was {
		was = l.s.PreviousPrice
	}
	l.s.PreviousPrice, l.s.PriceChangedAt = was, &now
}
