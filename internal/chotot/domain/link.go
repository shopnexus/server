package domain

import (
	"time"
	"uuid"
)

const (
	// SyncEvery is how often a link's listings are read in the background.
	SyncEvery = time.Hour
	// syncJitter spreads the links' checks, so they do not all come at once.
	syncJitter = 10 * time.Minute
	// maxSyncWait bounds the wait after Chợ Tốt did not answer.
	maxSyncWait = 8 * time.Hour
)

// Settings are what a link does after the first import; each starts on.
type Settings struct {
	// AutoImport brings the profile's new listings over.
	AutoImport bool `json:"auto_import"`
	// FollowUpdates hides a copy whose listing is gone from Chợ Tốt and
	// reprices one whose price changed there.
	FollowUpdates bool `json:"follow_updates"`
	// RemindHide tells the seller to hide a listing on Chợ Tốt once its
	// copy sells here.
	RemindHide bool `json:"remind_hide"`
}

// SettingsPatch names the settings to change; nil keeps one.
type SettingsPatch struct {
	AutoImport    *bool `json:"auto_import,omitempty"`
	FollowUpdates *bool `json:"follow_updates,omitempty"`
	RemindHide    *bool `json:"remind_hide,omitempty"`
}

// Link is an account's linked Chợ Tốt profile. ChototAccount is the number
// Chợ Tốt lists the profile's listings by. SeenThrough is the newest
// listing seen, nil before a read of the listings; Waiting the listings that
// could still be brought over at the last read.
type Link struct {
	Settings

	ID            uuid.UUID  `json:"id"`
	AccountID     uuid.UUID  `json:"account_id"`
	OID           string     `json:"oid"`
	ChototAccount int64      `json:"chotot_account"`
	Name          string     `json:"name"`
	AvatarURL     string     `json:"avatar_url"`
	SeenThrough   *int64     `json:"seen_through,omitempty"`
	Waiting       *int       `json:"waiting,omitempty"`
	LinkedAt      time.Time  `json:"linked_at"`
	CheckedAt     *time.Time `json:"checked_at,omitempty"`
}

// NewLink links a profile to an account with every setting on.
func NewLink(id, account uuid.UUID, oid string, chototAccount int64, name, avatar string, now time.Time) Link {
	return Link{
		ID: id, AccountID: account, OID: oid, ChototAccount: chototAccount, Name: name, AvatarURL: avatar,
		AutoImport: true, FollowUpdates: true, RemindHide: true, LinkedAt: now,
	}
}

// NextSync is the wait before a link's next check: every and up to
// syncJitter more, jitter being in [0, 1). After failures checks Chợ Tốt
// did not answer, every doubles each time, up to maxSyncWait.
func NextSync(every time.Duration, failures int, jitter float64) time.Duration {
	wait := every
	for range min(failures, 8) {
		wait *= 2
	}
	wait = min(wait, max(maxSyncWait, every))
	return wait + time.Duration(jitter*float64(min(syncJitter, every/6)))
}
