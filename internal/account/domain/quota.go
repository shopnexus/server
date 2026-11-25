package domain

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
type Quota struct{ s QuotaState }

// QuotaState is every stored field of a quota, and how a Restate step
// journals one it loaded. Stores only.
type QuotaState struct {
	Key     string      `json:"key"`
	SentAt  []time.Time `json:"sent_at"`
	WriteID uuid.UUID   `json:"write_id"` // zero until first saved
}

func NewQuota(key string) Quota { return Quota{s: QuotaState{Key: key}} }

// RestoreQuota rebuilds a stored quota. Stores only.
func RestoreQuota(st QuotaState) Quota { return Quota{s: st} }

func (q Quota) MarshalJSON() ([]byte, error) { return json.Marshal(q.s) }

func (q *Quota) UnmarshalJSON(b []byte) error {
	var st QuotaState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	*q = Quota{s: st}
	return nil
}

func (q *Quota) Key() string         { return q.s.Key }
func (q *Quota) SentAt() []time.Time { return slices.Clone(q.s.SentAt) }
func (q *Quota) WriteID() uuid.UUID  { return q.s.WriteID }

// Saved records that a store wrote q under writeID. Stores only.
func (q *Quota) Saved(writeID uuid.UUID) { q.s.WriteID = writeID }

// Admit records a send at now when every rule allows it. Otherwise it records
// nothing and returns the rule that refused and how long until that rule
// frees a slot. Sends older than the longest window are dropped.
func (q *Quota) Admit(now time.Time, rules ...Rule) (Rule, time.Duration, bool) {
	var longest time.Duration
	for _, r := range rules {
		longest = max(longest, r.Window)
	}
	q.s.SentAt = slices.DeleteFunc(q.s.SentAt, func(t time.Time) bool { return !t.After(now.Add(-longest)) })
	for _, r := range rules {
		var inWindow []time.Time
		for _, t := range q.s.SentAt {
			if t.After(now.Add(-r.Window)) {
				inWindow = append(inWindow, t)
			}
		}
		if len(inWindow) >= r.Max {
			return r, inWindow[0].Add(r.Window).Sub(now), false
		}
	}
	q.s.SentAt = append(q.s.SentAt, now)
	return Rule{}, 0, true
}
