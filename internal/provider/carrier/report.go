package carrier

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mime"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// vietnam is the zone of a carrier's time that names none.
var vietnam = time.FixedZone("ICT", 7*60*60)

// ReadReport reads a carrier's webhook: the tracking code and the events
// it tells, none for one that tells nothing ShopNexus keeps. contentType
// is the request's; GHTK posts a form or JSON, GHN JSON. now stands in for
// a time the report lacks.
func ReadReport(c Carrier, contentType string, body []byte, now time.Time) (code string, evs []Event, err error) {
	fields, err := reportFields(contentType, body)
	if err != nil {
		return "", nil, err
	}
	switch c {
	case GHTK:
		code, evs, err = ghtkReport(fields, now)
	case GHN:
		code, evs = ghnReport(fields, now)
	default:
		return "", nil, fmt.Errorf("no reports from %s", c)
	}
	if err == nil && code == "" {
		err = errors.New("the report names no code")
	}
	return code, evs, err
}

// reportFields flattens a report to its text and number fields.
func reportFields(contentType string, body []byte) (map[string]string, error) {
	fields := map[string]string{}
	if media, _, _ := mime.ParseMediaType(contentType); media != "application/json" {
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, fmt.Errorf("decode the report: %w", err)
		}
		for k := range values {
			fields[k] = values.Get(k)
		}
		return fields, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode the report: %w", err)
	}
	// GHTK sends status_id as 5 or "5".
	for k, v := range raw {
		switch v := v.(type) {
		case string:
			fields[k] = v
		case float64:
			fields[k] = strconv.FormatFloat(v, 'f', -1, 64)
		default:
		}
	}
	return fields, nil
}

// ghtkReport reads label_id and status_id, with the weight in kg, the fee
// and the COD that each report repeats.
func ghtkReport(f map[string]string, now time.Time) (string, []Event, error) {
	id, err := strconv.Atoi(strings.TrimSpace(f["status_id"]))
	if err != nil {
		return "", nil, fmt.Errorf("read status_id: %w", err)
	}
	at := readTime(f["action_time"], now)
	var evs []Event
	if st, ok := GHTKStep(id); ok {
		evs = append(evs, Event{
			At: at, Kind: KindStep, Step: st, CarrierStatus: strconv.Itoa(id),
			Reason: f["reason"], ReasonCode: f["reason_code"],
		})
	}
	if kg, ok := number(f["weight"]); ok {
		evs = append(evs, Event{At: at, Kind: KindWeight, Amount: int64(math.Round(kg * 1000))})
	}
	evs = appendAmount(evs, at, KindFee, f["fee"])
	evs = appendAmount(evs, at, KindCOD, f["pick_money"])
	return f["label_id"], evs, nil
}

// ghnReport reads OrderCode and its Type: a create or a switch_status
// tells a step, update_weight, update_fee and update_cod an amount. A
// paid-out COD comes with CODTransferDate on any of them.
func ghnReport(f map[string]string, now time.Time) (string, []Event) {
	at := readTime(f["Time"], now)
	var evs []Event
	switch strings.ToLower(f["Type"]) {
	case "", "create", "switch_status":
		if st, ok := GHNStep(f["Status"]); ok {
			evs = append(evs, Event{
				At: at, Kind: KindStep, Step: st, CarrierStatus: f["Status"],
				Reason: f["Reason"], ReasonCode: f["ReasonCode"], Warehouse: f["Warehouse"],
				ShipperName: f["ShipperName"], ShipperPhone: f["ShipperPhone"], ProofURL: f["PodURL"],
			})
		}
	case "update_weight":
		evs = appendAmount(evs, at, KindWeight, f["Weight"])
	case "update_fee":
		evs = appendAmount(evs, at, KindFee, f["TotalFee"])
	case "update_cod":
		evs = appendAmount(evs, at, KindCOD, f["CODAmount"])
	default: // another Type tells nothing kept
	}
	if paid := f["CODTransferDate"]; paid != "" {
		evs = appendAmount(evs, readTime(paid, now), KindCODTransfer, f["CODAmount"])
	}
	return f["OrderCode"], evs
}

func appendAmount(evs []Event, at time.Time, k EventKind, s string) []Event {
	if n, ok := number(s); ok {
		return append(evs, Event{At: at, Kind: k, Amount: int64(math.Round(n))})
	}
	return evs
}

func number(s string) (float64, bool) {
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return n, err == nil && n >= 0
}

// readTime reads RFC 3339, or GHTK's "2006-01-02 15:04:05" in Vietnam's
// zone; now for none.
func readTime(s string, now time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.ParseInLocation(time.DateTime, s, vietnam); err == nil {
		return t
	}
	return now
}
