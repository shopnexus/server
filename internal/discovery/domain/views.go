package domain

import "time"

// ViewDay is the UTC day a listing view at t counts on: a person counts once
// per listing and day.
func ViewDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ViewWeek is the start of the seven UTC days that end on now's day, the
// days views are stored by: a week of views, saves and askers counts from
// it.
func ViewWeek(now time.Time) time.Time {
	return ViewDay(now).AddDate(0, 0, -6)
}
