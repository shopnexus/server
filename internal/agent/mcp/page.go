package mcp

import (
	"uuid"

	"github.com/shopnexus/server/internal/platform/page"
)

// maxPage caps a list tool's page, so one call cannot fill the agent's
// context.
const maxPage = 50

// pageIn is the cursor and size of a list tool, embedded in its input.
type pageIn struct {
	Cursor uuid.UUID `json:"cursor,omitzero" jsonschema:"the next_cursor of the previous page; leave out for the first page"`
	Limit  int       `json:"limit,omitempty" jsonschema:"items per page, 1 to 50; 20 when left out"`
}

// request is the page the services read, the size kept within maxPage.
func (p pageIn) request() page.Request {
	return page.Request{Cursor: p.Cursor, Limit: min(max(p.Limit, 0), maxPage)}
}

// pageOut is one page of a list tool's answer.
type pageOut[T any] struct {
	Items      []T        `json:"items"`
	NextCursor *uuid.UUID `json:"next_cursor,omitempty" jsonschema:"pass it as cursor for the next page; absent on the last page"`
}

// pageOf maps a service's page to a tool's, item by item.
func pageOf[S, T any](p page.Page[S], f func(*S) T) pageOut[T] {
	out := pageOut[T]{Items: make([]T, 0, len(p.Items)), NextCursor: p.NextCursor}
	for i := range p.Items {
		out.Items = append(out.Items, f(&p.Items[i]))
	}
	return out
}
