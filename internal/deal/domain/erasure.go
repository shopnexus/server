package domain

import "time"

// KeepRecipientsFor is how long after an account's deletion its orders
// keep the recipient's name, phone and street.
const KeepRecipientsFor = 365 * 24 * time.Hour

// PartyLeft ends what a deleted account leaves open on the deal, by the
// system: a pending order is cancelled, an agreement dropped, an open
// price taken back. A shipped order stays; erasure refuses a deletion
// while one exists. It reports whether the deal changed.
func (d *Deal) PartyLeft(now time.Time) bool {
	if o := d.openOrder(); o != nil {
		if o.Status != OrderPending {
			return false
		}
		d.cancelOrder(System, CancelListingClosed, now)
		return true
	}
	switch {
	case d.s.Status == StatusAgreed:
		d.reopen()
	case d.s.Open != nil:
		d.s.Open = nil
	default:
		return false
	}
	d.record(MoveListingClosed, System, 0, now)
	return true
}
