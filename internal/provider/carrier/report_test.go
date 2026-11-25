package carrier_test

import (
	"testing"
	"time"

	"github.com/shopnexus/server/internal/provider/carrier"
)

func TestReadReportTellsStepsAndAmounts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		carrier carrier.Carrier
		media   string
		body    string
		want    []carrier.Event
	}{
		"a GHTK form with its weight, fee and COD": {
			carrier.GHTK, "application/x-www-form-urlencoded",
			"label_id=S1.A1&status_id=9&action_time=2026-11-24T16:00:00%2B07:00&reason=Kh%C3%B4ng+nghe+m%C3%A1y&weight=0.8&fee=30400&pick_money=47000",
			[]carrier.Event{
				{At: now, Kind: carrier.KindStep, Step: carrier.StepDeliveryFailed, CarrierStatus: "9", Reason: "Không nghe máy"},
				{At: now, Kind: carrier.KindWeight, Amount: 800},
				{At: now, Kind: carrier.KindFee, Amount: 30_400},
				{At: now, Kind: carrier.KindCOD, Amount: 47_000},
			},
		},
		"a GHTK shipper's notice": {carrier.GHTK, "application/json", `{"label_id":"S1","status_id":"123"}`, nil},
		"a GHN exception":         {carrier.GHN, "application/json", `{"OrderCode":"LY9VHQ","Type":"switch_status","Status":"exception"}`, nil},
		"a GHN switch_status": {
			carrier.GHN, "application/json",
			`{"OrderCode":"LY9VHQ","Type":"switch_status","Status":"storing","Time":"2026-11-24T09:00:00Z","Warehouse":"Bưu Cục Quận 1"}`,
			[]carrier.Event{{At: now, Kind: carrier.KindStep, Step: carrier.StepAtHub, CarrierStatus: "storing", Warehouse: "Bưu Cục Quận 1"}},
		},
		"a GHN fee whose Status is stale": {
			carrier.GHN, "application/json", `{"OrderCode":"LY9VHQ","Type":"update_fee","Status":"picked","TotalFee":20900,"Fee":{"MainService":20900}}`,
			[]carrier.Event{{At: now, Kind: carrier.KindFee, Amount: 20_900}},
		},
		"a GHN COD paid out": {
			carrier.GHN, "application/json",
			`{"OrderCode":"LY9VHQ","Type":"switch_status","Status":"delivered","CODAmount":47000,"CODTransferDate":"2026-11-24T09:00:00Z"}`,
			[]carrier.Event{
				{At: now, Kind: carrier.KindStep, Step: carrier.StepDelivered, CarrierStatus: "delivered"},
				{At: now, Kind: carrier.KindCODTransfer, Amount: 47_000},
			},
		},
	} {
		code, evs, err := carrier.Fake{Carrier: c.carrier}.Report(c.media, []byte(c.body), now)
		if err != nil || code == "" || len(evs) != len(c.want) {
			t.Errorf("%s: %q %+v %v", name, code, evs, err)
			continue
		}
		for i := range evs {
			if !evs[i].At.Equal(c.want[i].At) {
				t.Errorf("%s: event %d at %s", name, i, evs[i].At)
			}
			evs[i].At = c.want[i].At
			if evs[i] != c.want[i] {
				t.Errorf("%s: event %d %+v", name, i, evs[i])
			}
		}
	}
	for _, body := range []string{`{"status_id":5}`, `not json`} {
		if _, _, err := (carrier.GHTKClient{}).Report("application/json", []byte(body), now); err == nil {
			t.Errorf("%s was read", body)
		}
	}
}

// A failed delivery stays in transit: only a return fails the order, and a
// cancel or a loss moves none.
func TestStepsMapToTheOrderStatus(t *testing.T) {
	t.Parallel()
	for st, want := range map[carrier.Step]carrier.Status{
		carrier.StepPicking: carrier.StatusPendingPickup, carrier.StepDeliveryFailed: carrier.StatusInTransit,
		carrier.StepDelivered: carrier.StatusDelivered, carrier.StepReturning: carrier.StatusFailed,
	} {
		if got, ok := st.Status(); !ok || got != want {
			t.Errorf("%s: %s %v", st, got, ok)
		}
	}
	for _, st := range []carrier.Step{carrier.StepCancelled, carrier.StepLost, ""} {
		if got, ok := st.Status(); ok {
			t.Errorf("%q moves the order to %s", st, got)
		}
	}
}

// A parcel cancelled or lost reports lost; every other step reports its
// status, and no step none.
func TestStepReported(t *testing.T) {
	t.Parallel()
	for step, want := range map[carrier.Step]carrier.Status{
		carrier.StepCancelled: carrier.StatusLost, carrier.StepLost: carrier.StatusLost,
		carrier.StepPicked: carrier.StatusInTransit, carrier.StepReturned: carrier.StatusFailed,
	} {
		if got, ok := step.Reported(); !ok || got != want {
			t.Errorf("%s: %s %v", step, got, ok)
		}
		if st, _ := step.Status(); st == carrier.StatusLost {
			t.Errorf("%s reads as lost", step)
		}
	}
	if _, ok := carrier.Step("").Reported(); ok {
		t.Error("no step reports a status")
	}
}
