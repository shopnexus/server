package domain

import "time"

const (
	// ReplyPeriod is how far back a reply rate reaches.
	ReplyPeriod = 90 * 24 * time.Hour
	// ReplyWindow is how soon a reply counts as in time, and how old an
	// unanswered conversation must be to count.
	ReplyWindow = 24 * time.Hour
	// MinReplyCount is how many conversations a reply rate needs.
	MinReplyCount = 5
)

// ReplyThread is a conversation another person started about one of a
// seller's listings: when its first message came, and when the seller first
// wrote in it, nil before they did.
type ReplyThread struct {
	Started  time.Time
	Answered *time.Time
}

// ReplyRateOf weighs a seller's threads at now. A thread counts when it
// started within ReplyPeriod and is either answered or older than
// ReplyWindow; it is in time when the answer came within ReplyWindow of
// its start. percent is the share in time, rounded down; ok is false with
// fewer than MinReplyCount threads counted.
func ReplyRateOf(threads []ReplyThread, now time.Time) (percent, count int, ok bool) {
	inTime := 0
	for _, t := range threads {
		if t.Started.Before(now.Add(-ReplyPeriod)) {
			continue
		}
		if t.Answered == nil {
			if now.Sub(t.Started) <= ReplyWindow {
				continue
			}
			count++
			continue
		}
		count++
		if t.Answered.Sub(t.Started) <= ReplyWindow {
			inTime++
		}
	}
	if count < MinReplyCount {
		return 0, count, false
	}
	return inTime * 100 / count, count, true
}
