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

// ParseStep reads a step's name; false for another.
func ParseStep(s string) (Step, bool) {
	st := Step(s)
	if _, moves := st.Status(); moves || st == StepCancelled || st == StepLost {
		return st, true
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

// ghtkSteps maps GHTK's status_id. The shipper's notices (123, 127, 128,
// 45, 49, 410) come before GHTK's own status and map to none.
var ghtkSteps = map[int]Step{
	-1: StepCancelled, 1: StepReadyToPick, 2: StepReadyToPick, 12: StepPicking,
	7: StepPickupFailed, 8: StepPickupFailed, 3: StepPicked, 4: StepOutForDelivery,
	9: StepDeliveryFailed, 10: StepDeliveryFailed, 5: StepDelivered, 6: StepDelivered,
	20: StepReturning, 11: StepReturned, 21: StepReturned, 13: StepLost,
}

// ghnSteps maps GHN's order status; exception maps to none.
var ghnSteps = map[string]Step{
	"ready_to_pick": StepReadyToPick, "picking": StepPicking, "money_collect_picking": StepPicking,
	"picked": StepPicked, "storing": StepAtHub, "sorting": StepAtHub, "transporting": StepTransporting,
	"delivering": StepOutForDelivery, "money_collect_delivering": StepOutForDelivery,
	"delivery_fail": StepDeliveryFailed, "delivered": StepDelivered,
	"waiting_to_return": StepReturning, "return": StepReturning, "return_transporting": StepReturning,
	"return_sorting": StepReturning, "returning": StepReturning, "return_fail": StepReturning,
	"returned": StepReturned, "cancel": StepCancelled, "lost": StepLost, "damage": StepLost,
}

// GHTKStep maps GHTK's status_id; false for one that maps to no step.
func GHTKStep(id int) (Step, bool) {
	s, ok := ghtkSteps[id]
	return s, ok
}

// GHNStep maps GHN's order status; false for one that maps to no step.
func GHNStep(s string) (Step, bool) {
	st, ok := ghnSteps[s]
	return st, ok
}
