package domain

import (
	"time"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/locale"
)

// ErrInvalidQuietHours is quiet hours that start when they end, or a time
// that is not HH:MM.
var ErrInvalidQuietHours = apperr.New(apperr.KindBadRequest, "invalid_quiet_hours",
	"quiet hours run from one HH:MM to another")

// Switch names the setting that stops the pushes of a kind of notice.
type Switch string

const (
	SwitchMessages  Switch = "messages"
	SwitchOffers    Switch = "offers"
	SwitchOrders    Switch = "orders"
	SwitchPriceCuts Switch = "price_cuts"
	SwitchFollowed  Switch = "followed"
	SwitchReviews   Switch = "reviews"
	// SwitchNone is no switch: only quiet hours hold the push.
	SwitchNone Switch = ""
)

// Settings say which notices push, and whether order notices are mailed. A
// switch that is off stops the push or the mail only; the notice is still
// listed.
type Settings struct {
	Messages    bool  `json:"messages"`
	Offers      bool  `json:"offers"`
	Orders      bool  `json:"orders"`
	PriceCuts   bool  `json:"price_cuts"`
	Followed    bool  `json:"followed"`
	Reviews     bool  `json:"reviews"`
	OrderEmails bool  `json:"order_emails"`
	Quiet       Quiet `json:"quiet"`
}

// Quiet is a daily window without pushes, in Vietnam time, from From up to
// To, both HH:MM; a From later than To crosses midnight.
type Quiet struct {
	On   bool   `json:"on"`
	From string `json:"from"`
	To   string `json:"to"`
}

// DefaultSettings are an account's until it saves its own: every switch
// on, quiet from 22:00 to 07:00.
func DefaultSettings() Settings {
	return Settings{
		Messages: true, Offers: true, Orders: true, PriceCuts: true, Followed: true, Reviews: true, OrderEmails: true,
		Quiet: Quiet{On: true, From: "22:00", To: "07:00"},
	}
}

// Check refuses quiet hours whose times are not HH:MM or are the same,
// whether or not they are on.
func (s Settings) Check() error {
	from, okFrom := minuteOfDay(s.Quiet.From)
	to, okTo := minuteOfDay(s.Quiet.To)
	if !okFrom || !okTo || from == to {
		return ErrInvalidQuietHours
	}
	return nil
}

// Pushes reports whether a push under sw goes out at at: its switch is on,
// and quiet hours are off or do not hold at.
func (s Settings) Pushes(sw Switch, at time.Time) bool {
	on := true
	switch sw {
	case SwitchMessages:
		on = s.Messages
	case SwitchOffers:
		on = s.Offers
	case SwitchOrders:
		on = s.Orders
	case SwitchPriceCuts:
		on = s.PriceCuts
	case SwitchFollowed:
		on = s.Followed
	case SwitchReviews:
		on = s.Reviews
	case SwitchNone:
	default:
	}
	return on && !s.Quiet.Holds(at)
}

// Mails reports whether a notice of type t is mailed: an order notice
// while OrderEmails is on, an account notice always. Quiet hours hold no
// mail.
func (s Settings) Mails(t Type) bool {
	m := types[t].mail
	return m == mailAlways || m == mailOrders && s.OrderEmails
}

// Holds reports whether at falls in the window, From included and To not,
// read on Vietnam's clock. A window that is off, or not valid, holds
// nothing.
func (q Quiet) Holds(at time.Time) bool {
	from, okFrom := minuteOfDay(q.From)
	to, okTo := minuteOfDay(q.To)
	if !q.On || !okFrom || !okTo {
		return false
	}
	local := at.In(locale.Vietnam)
	m := local.Hour()*60 + local.Minute()
	if from < to {
		return from <= m && m < to
	}
	return m >= from || m < to
}

// minuteOfDay reads HH:MM on a 24-hour clock as minutes after midnight.
func minuteOfDay(s string) (int, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != len("15:04") {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}
