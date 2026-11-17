package domain

import (
	"cmp"
	"slices"
	"uuid"
)

// A report case is the reports about one account that share one
// resolution. The reports neither withdrawn nor resolved are the account's
// open case; resolving it stamps them with a Resolution, and the account's
// next report opens a new case.

// CaseStatus is whether a case is open or resolved.
type CaseStatus string

const (
	CaseOpen     CaseStatus = "open"
	CaseResolved CaseStatus = "resolved"
)

// Outcome is how a console member resolved a case: dismissed ("Không vi
// phạm") or actioned, after the member hid listings or suspended the
// account.
type Outcome string

const (
	OutcomeDismissed Outcome = "dismissed"
	OutcomeActioned  Outcome = "actioned"
)

// ListingTally is how often one listing is reported in a case, and the ID of
// its latest report there.
type ListingTally struct {
	ListingID uuid.UUID
	Reports   int
	LatestID  uuid.UUID
}

// RankListings orders a case's listings most reported first, a tie going
// to the one reported last. Report IDs are UUIDv7, so the larger ID is the
// later report.
func RankListings(tallies []ListingTally) []uuid.UUID {
	sorted := slices.Clone(tallies)
	slices.SortFunc(sorted, func(a, b ListingTally) int {
		if c := cmp.Compare(b.Reports, a.Reports); c != 0 {
			return c
		}
		return b.LatestID.Compare(a.LatestID)
	})
	out := make([]uuid.UUID, len(sorted))
	for i := range sorted {
		out[i] = sorted[i].ListingID
	}
	return out
}
