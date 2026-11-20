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
		tooOld := t.Started.Before(now.Add(-ReplyPeriod))
		tooYoung := t.Answered == nil && now.Sub(t.Started) <= ReplyWindow
		if tooOld || tooYoung {
			continue
		}
		count++
		if t.Answered != nil && t.Answered.Sub(t.Started) <= ReplyWindow {
			inTime++
		}
	}
	if count < MinReplyCount {
		return 0, count, false
	}
	return inTime * 100 / count, count, true
}

const (
	// ReplyTimePeriod is how far back a reply time reaches.
	ReplyTimePeriod = 30 * 24 * time.Hour
	// ReplyGap is how long after the seller's last message another one
	// from the other person starts a new wait; a message sooner continues
	// the talk.
	ReplyGap = time.Hour
	// MinReplySamples is how many answers a reply time needs.
	MinReplySamples = 5
)

// ReplyTime is how soon a seller usually answers: the ReplyTime schema.
type ReplyTime string

const (
	ReplyMinutes ReplyTime = "minutes"
	ReplyHour    ReplyTime = "hour"
	ReplyHours   ReplyTime = "hours"
	ReplyDay     ReplyTime = "day"
)

// replyBounds are the longest median wait of each ReplyTime, shortest first.
var replyBounds = []struct {
	upTo time.Duration
	time ReplyTime
}{
	{10 * time.Minute, ReplyMinutes},
	{time.Hour, ReplyHour},
	{6 * time.Hour, ReplyHours},
	{24 * time.Hour, ReplyDay},
}

// ReplyTimeOf is the ReplyTime of a median wait over samples answers; ok
// is false below MinReplySamples or past a day.
func ReplyTimeOf(samples int, median time.Duration) (t ReplyTime, ok bool) {
	if samples < MinReplySamples {
		return "", false
	}
	for _, b := range replyBounds {
		if median <= b.upTo {
			return b.time, true
		}
	}
	return "", false
}
