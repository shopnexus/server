package otp

import (
	"encoding/json"
	"slices"
	"time"
	"uuid"
)

// Rule allows at most Max sends within Window.
type Rule struct {
	Window time.Duration
	Max    int
}

// Quota is the recent sends of one destination ("dest:...") or one client
// address ("ip:...").
type Quota struct {
	key     string
	sentAt  []time.Time
	writeID uuid.UUID // zero until first saved
}

func NewQuota(key string) Quota { return Quota{key: key, sentAt: nil, writeID: uuid.UUID{}} }

// RestoreQuota rebuilds a stored quota. Stores only.
func RestoreQuota(key string, sentAt []time.Time, writeID uuid.UUID) Quota {
	return Quota{key: key, sentAt: sentAt, writeID: writeID}
}

// quotaJSON is how a Restate step journals a quota it loaded.
type quotaJSON struct {
	Key     string      `json:"key"`
	SentAt  []time.Time `json:"sent_at"`
	WriteID uuid.UUID   `json:"write_id"`
}

func (q Quota) MarshalJSON() ([]byte, error) {
	return json.Marshal(quotaJSON{q.key, q.sentAt, q.writeID})
}

func (q *Quota) UnmarshalJSON(b []byte) error {
	var j quotaJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*q = RestoreQuota(j.Key, j.SentAt, j.WriteID)
	return nil
}

func (q *Quota) Key() string         { return q.key }
func (q *Quota) SentAt() []time.Time { return slices.Clone(q.sentAt) }
func (q *Quota) WriteID() uuid.UUID  { return q.writeID }

// Admit records a send at now when every rule allows it. Otherwise it records
// nothing and returns the rule that refused and how long until that rule
// frees a slot. Sends older than the longest window are dropped.
func (q *Quota) Admit(now time.Time, rules ...Rule) (Rule, time.Duration, bool) {
	var longest time.Duration
	for _, r := range rules {
		longest = max(longest, r.Window)
	}
	q.sentAt = slices.DeleteFunc(q.sentAt, func(t time.Time) bool { return !t.After(now.Add(-longest)) })
	for _, r := range rules {
		var inWindow []time.Time
		for _, t := range q.sentAt {
			if t.After(now.Add(-r.Window)) {
				inWindow = append(inWindow, t)
			}
		}
		if len(inWindow) >= r.Max {
			return r, inWindow[0].Add(r.Window).Sub(now), false
		}
	}
	q.sentAt = append(q.sentAt, now)
	return Rule{}, 0, true
}
