// Package page is keyset pagination over UUIDv7 IDs, newest first. A page's
// cursor is the ID of its last item, and the next page lists the IDs below
// it. A store reads one row more than the page holds; that row says whether
// a next page exists and is cut before the response.
package page

import "uuid"

// DefaultLimit is the page size when the request names none; the spec
// declares the same default.
const DefaultLimit = 20

// Request is the cursor and limit of a list endpoint. A handler binds it
// from the query, alone or embedded in its request struct, and passes it to
// the use case and the store.
type Request struct {
	Cursor uuid.UUID `json:"-" query:"cursor"`
	Limit  int       `json:"-" query:"limit"`
}

// Before is the ID the store lists below: the cursor, or [uuid.Max] on the
// first page, so the query needs no case for a missing cursor.
func (r Request) Before() uuid.UUID {
	if r.Cursor == (uuid.UUID{}) {
		return uuid.Max()
	}
	return r.Cursor
}

// Fetch is how many rows the store reads: the page size plus one.
func (r Request) Fetch() int { return r.size() + 1 }

func (r Request) size() int {
	if r.Limit <= 0 {
		return DefaultLimit
	}
	return r.Limit
}

// Page is one page of a list response.
type Page[T any] struct {
	Items      []T        `json:"items"`
	NextCursor *uuid.UUID `json:"next_cursor,omitempty"` //exhaustruct:optional
}

// Cut trims rows, read with r.Fetch(), to the page size and reports whether
// more rows follow. The caller sets NextCursor to the ID of the last row it
// keeps when more is true.
func Cut[T any](rows []T, r Request) (kept []T, more bool) {
	if len(rows) > r.size() {
		return rows[:r.size()], true
	}
	return rows, false
}
