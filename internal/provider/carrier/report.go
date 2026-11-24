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

	"github.com/shopnexus/server/internal/platform/locale"
)

// errNoCode is a report that names no parcel.
var errNoCode = errors.New("the report names no code")

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
	if t, err := time.ParseInLocation(time.DateTime, s, locale.Vietnam); err == nil {
		return t
	}
	return now
}
