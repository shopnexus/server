package carrier

import "time"

// Step is where a parcel is, in ShopNexus's words for every carrier: finer
// than Status, which is what moves an order.
type Step string

const (
	StepReadyToPick    Step = "ready_to_pick"
	StepPicking        Step = "picking"
	StepPickupFailed   Step = "pickup_failed"
	StepPicked         Step = "picked"
	StepAtHub          Step = "at_hub"
	StepTransporting   Step = "transporting"
	StepOutForDelivery Step = "out_for_delivery"
	StepDeliveryFailed Step = "delivery_failed"
	StepDelivered      Step = "delivered"
	StepReturning      Step = "returning"
	StepReturned       Step = "returned"
	StepCancelled      Step = "cancelled"
	StepLost           Step = "lost"
)

// Status is the order's status at step; false for a step that moves no
// order (cancelled, lost, or none). A failed delivery is still in transit:
// the carrier tries again, and only a return fails the order.
func (s Step) Status() (Status, bool) {
	switch s {
	case StepReadyToPick, StepPicking, StepPickupFailed:
		return StatusPendingPickup, true
	case StepPicked, StepAtHub, StepTransporting, StepOutForDelivery, StepDeliveryFailed:
		return StatusInTransit, true
	case StepDelivered:
		return StatusDelivered, true
	case StepReturning, StepReturned:
		return StatusFailed, true
	case StepCancelled, StepLost:
	default: // none
	}
	return "", false
}

// EventKind is what an event tells: a step, or a new weight, fee or COD
// amount, or the COD paid out to the seller.
type EventKind string

const (
	KindStep        EventKind = "step"
	KindWeight      EventKind = "weight"
	KindFee         EventKind = "fee"
	KindCOD         EventKind = "cod"
	KindCODTransfer EventKind = "cod_transfer"
)

// Event is one thing a carrier reported of a parcel. CarrierStatus is the
// carrier's own word for it, kept so a client can tell more than the step.
type Event struct {
	At            time.Time `json:"at"`
	Kind          EventKind `json:"kind"`
	Step          Step      `json:"step,omitempty"`
	CarrierStatus string    `json:"carrier_status,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	ReasonCode    string    `json:"reason_code,omitempty"`
	Warehouse     string    `json:"warehouse,omitempty"`
	ShipperName   string    `json:"shipper_name,omitempty"`
	ShipperPhone  string    `json:"shipper_phone,omitempty"`
	ProofURL      string    `json:"proof_url,omitempty"`
	// Amount is the fee or the COD in đồng, or the weight in grams.
	Amount int64 `json:"amount,omitempty"`
}
